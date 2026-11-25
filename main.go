// Copyright (c) 2025 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	libboltkv "github.com/bborbe/boltkv"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libhttp "github.com/bborbe/http"
	libkafka "github.com/bborbe/kafka"
	libkv "github.com/bborbe/kv"
	"github.com/bborbe/log"
	libmetrics "github.com/bborbe/metrics"
	"github.com/bborbe/run"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/bborbe/k8s-apply/pkg/apply"
	"github.com/bborbe/k8s-apply/pkg/factory"
)

const serviceName = "k8s-apply"

// overrideUnset is the sentinel an unset override carries: the arg/env parser
// has no nullable field type, so "keep the built-in value" is spelled as a
// value no real override would use.
const overrideUnset = "-"

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN          string            `required:"true"  arg:"sentry-dsn"           env:"SENTRY_DSN"           usage:"SentryDSN"                                                                                  display:"length"`
	SentryProxy        string            `required:"false" arg:"sentry-proxy"         env:"SENTRY_PROXY"         usage:"Sentry Proxy"`
	Listen             string            `required:"true"  arg:"listen"               env:"LISTEN"               usage:"address to listen to"`
	KafkaBrokers       string            `required:"true"  arg:"kafka-brokers"        env:"KAFKA_BROKERS"        usage:"Comma separated list of Kafka brokers"`
	BatchSize          int               `required:"true"  arg:"batch-size"           env:"BATCH_SIZE"           usage:"batch consume size"                                                                                          default:"1"`
	DataDir            string            `required:"true"  arg:"datadir"              env:"DATADIR"              usage:"data directory"`
	Stage              string            `required:"true"  arg:"stage"                env:"STAGE"                usage:"stage whose apply commands this instance consumes; a label, any non-empty name is accepted"                  default:"dev"`
	ApplyBranch        string            `required:"false" arg:"apply-branch"         env:"APPLY_BRANCH"         usage:"override the branch passed to make as BRANCH; '-' keeps the stage's built-in value"                          default:"-"`
	TopicPrefix        string            `required:"false" arg:"topic-prefix"         env:"TOPIC_PREFIX"         usage:"override the Kafka topic prefix; '-' keeps the stage's built-in value"                                       default:"-"`
	RequiresMasterHead string            `required:"false" arg:"requires-master-head" env:"REQUIRES_MASTER_HEAD" usage:"override the master-head guard (true|false); '-' keeps the stage's built-in value"                           default:"-"`
	RefreshCheckout    string            `required:"false" arg:"refresh-checkout"     env:"REFRESH_CHECKOUT"     usage:"override the pre-apply checkout refresh (true|false); '-' keeps the stage's built-in value"                  default:"-"`
	RepoDir            string            `required:"true"  arg:"repo-dir"             env:"REPO_DIR"             usage:"path to the deploy repo checkout this stage applies from"                                                    default:"/repo"`
	Namespace          string            `required:"false" arg:"namespace"            env:"NAMESPACE"            usage:"namespace whose Deployments the post-apply health check reads; defaults to the pod's own"`
	HealthTimeout      time.Duration     `required:"true"  arg:"health-timeout"       env:"HEALTH_TIMEOUT"       usage:"how long a Deployment may take to become healthy after an apply before it is rolled back"                    default:"5m"`
	BuildGitVersion    string            `required:"false" arg:"build-git-version"    env:"BUILD_GIT_VERSION"    usage:"Build Git version"                                                                                           default:"dev"`
	BuildGitCommit     string            `required:"false" arg:"build-git-commit"     env:"BUILD_GIT_COMMIT"     usage:"Build Git commit hash"                                                                                       default:"none"`
	BuildDate          *libtime.DateTime `required:"false" arg:"build-date"           env:"BUILD_DATE"           usage:"Build timestamp (RFC3339)"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
	stage, err := apply.ParseStage(ctx, a.Stage)
	if err != nil {
		return errors.Wrap(ctx, err, "parse stage failed")
	}
	config, err := a.stageConfig(ctx, stage)
	if err != nil {
		return err
	}

	libmetrics.NewBuildInfoMetrics().SetBuildInfo(a.BuildGitVersion, a.BuildGitCommit, a.BuildDate)

	saramaClientProvider, err := libkafka.NewSaramaClientProviderByType(
		ctx,
		libkafka.SaramaClientProviderTypeReused,
		libkafka.ParseBrokersFromString(a.KafkaBrokers),
	)
	if err != nil {
		return errors.Wrap(ctx, err, "create sarama client provider failed")
	}
	defer saramaClientProvider.Close()

	syncProducer, err := libkafka.NewSyncProducerWithName(
		ctx,
		libkafka.ParseBrokersFromString(a.KafkaBrokers),
		serviceName,
	)
	if err != nil {
		return errors.Wrap(ctx, err, "create sync producer failed")
	}
	defer syncProducer.Close()

	db, err := libboltkv.OpenDir(ctx, a.DataDir)
	if err != nil {
		return errors.Wrap(ctx, err, "open db failed")
	}
	defer db.Close()

	deploymentClient, err := a.createDeploymentClient(ctx)
	if err != nil {
		return err
	}

	return service.Run(
		ctx,
		factory.CreateCommandConsumer(
			saramaClientProvider,
			syncProducer,
			db,
			stage,
			config,
			a.RepoDir,
			deploymentClient,
			a.HealthTimeout,
		),
		a.createHTTPServer(db),
	)

}

// stageConfig resolves the behaviour this instance runs with: the stage's
// built-in defaults with every override the deploy set applied on top.
//
// The arg/env parser does not support pointer fields, so each override is a
// string whose sentinel default "-" means "keep the stage's built-in value".
func (a *application) stageConfig(
	ctx context.Context,
	stage apply.Stage,
) (apply.StageConfig, error) {
	overrides := apply.StageOverrides{}
	if a.ApplyBranch != overrideUnset {
		value := a.ApplyBranch
		overrides.ApplyBranch = &value
	}
	if a.TopicPrefix != overrideUnset {
		prefix := base.TopicPrefix(a.TopicPrefix)
		overrides.TopicPrefix = &prefix
	}
	if a.RequiresMasterHead != overrideUnset {
		value, err := strconv.ParseBool(a.RequiresMasterHead)
		if err != nil {
			return apply.StageConfig{}, errors.Wrapf(
				ctx,
				err,
				"parse requires-master-head '%s'",
				a.RequiresMasterHead,
			)
		}
		overrides.RequiresMasterHead = &value
	}
	if a.RefreshCheckout != overrideUnset {
		value, err := strconv.ParseBool(a.RefreshCheckout)
		if err != nil {
			return apply.StageConfig{}, errors.Wrapf(
				ctx,
				err,
				"parse refresh-checkout '%s'",
				a.RefreshCheckout,
			)
		}
		overrides.RefreshCheckout = &value
	}
	config := apply.ResolveStageConfig(stage, overrides)
	glog.V(2).Infof(
		"stage '%s' resolved: apply-branch '%s', topic-prefix '%s', requires-master-head %t, refresh-checkout %t",
		string(stage),
		config.ApplyBranch,
		config.TopicPrefix.String(),
		config.RequiresMasterHead,
		config.RefreshCheckout,
	)
	return config, nil
}

// serviceAccountNamespaceFile is where every in-cluster pod finds its own
// namespace.
const serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// createDeploymentClient builds the in-cluster client the post-apply health
// check reads and restores Deployments through. Its Role is namespaced, so the
// client is scoped to the pod's own namespace unless NAMESPACE overrides it.
//
// Running outside a cluster is a startup failure rather than a silently
// skipped check: an apply service that cannot observe the stage would report
// success on every broken rollout, which is the defect this check exists for.
func (a *application) createDeploymentClient(ctx context.Context) (apply.DeploymentClient, error) {
	namespace := a.Namespace
	if namespace == "" {
		content, err := os.ReadFile(serviceAccountNamespaceFile)
		if err != nil {
			return nil, errors.Wrapf(ctx, err, "read pod namespace failed; set NAMESPACE")
		}
		namespace = strings.TrimSpace(string(content))
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "create in-cluster config failed")
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "create kubernetes clientset failed")
	}
	return apply.NewDeploymentClient(clientset, namespace), nil
}

func (a *application) createHTTPServer(
	db libkv.DB,
) run.Func {
	return func(ctx context.Context) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		router := mux.NewRouter()
		router.Path("/healthz").Handler(factory.CreateHealthzHandler())
		router.Path("/readiness").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/metrics").Handler(promhttp.Handler())
		router.Path("/resetdb").Handler(libkv.NewResetHandler(db, cancel))
		router.Path("/resetbucket/{BucketName}").Handler(libkv.NewResetBucketHandler(db, cancel))
		router.Path("/setloglevel/{level}").
			Handler(log.NewSetLoglevelHandler(ctx, log.NewLogLevelSetter(2, 5*time.Minute)))
		router.Path("/gc").Handler(libhttp.NewGarbageCollectorHandler())

		glog.V(2).Infof("starting http server listen on %s", a.Listen)
		return libhttp.NewServer(
			a.Listen,
			router,
		).Run(ctx)
	}
}
