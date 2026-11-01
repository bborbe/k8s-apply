// Copyright (c) 2025 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"net/url"
	"os"
	"strings"
	"time"

	libboltkv "github.com/bborbe/boltkv"
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

	"github.com/bborbe/nuke-apply/pkg/apply"
	"github.com/bborbe/nuke-apply/pkg/factory"
)

const serviceName = "nuke-apply"

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN       string            `required:"true"  arg:"sentry-dsn"        env:"SENTRY_DSN"        usage:"SentryDSN"                                                            display:"length"`
	SentryProxy     string            `required:"false" arg:"sentry-proxy"      env:"SENTRY_PROXY"      usage:"Sentry Proxy"`
	Listen          string            `required:"true"  arg:"listen"            env:"LISTEN"            usage:"address to listen to"`
	KafkaBrokers    string            `required:"true"  arg:"kafka-brokers"     env:"KAFKA_BROKERS"     usage:"Comma separated list of Kafka brokers"`
	BatchSize       int               `required:"true"  arg:"batch-size"        env:"BATCH_SIZE"        usage:"batch consume size"                                                                    default:"1"`
	DataDir         string            `required:"true"  arg:"datadir"           env:"DATADIR"           usage:"data directory"`
	Stage           string            `required:"true"  arg:"stage"             env:"STAGE"             usage:"stage whose apply commands this instance consumes"                                     default:"dev"`
	AttentionURL    string            `required:"false" arg:"attention-url"     env:"ATTENTION_URL"     usage:"attention store base URL, required on a stage that needs an approval"`
	AttentionToken  string            `required:"false" arg:"attention-token"   env:"ATTENTION_TOKEN"   usage:"bearer token for the attention store's gated listener"                display:"length"`
	RepoDir         string            `required:"true"  arg:"repo-dir"          env:"NUKE_REPO_DIR"     usage:"path to the deploy repo checkout this stage applies from"                              default:"/repo"`
	BuildGitVersion string            `required:"false" arg:"build-git-version" env:"BUILD_GIT_VERSION" usage:"Build Git version"                                                                     default:"dev"`
	BuildGitCommit  string            `required:"false" arg:"build-git-commit"  env:"BUILD_GIT_COMMIT"  usage:"Build Git commit hash"                                                                 default:"none"`
	BuildDate       *libtime.DateTime `required:"false" arg:"build-date"        env:"BUILD_DATE"        usage:"Build timestamp (RFC3339)"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
	stage, err := apply.ParseStage(ctx, a.Stage)
	if err != nil {
		return errors.Wrap(ctx, err, "parse stage failed")
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

	approver, err := a.createApprover(ctx, stage)
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
			a.RepoDir,
			approver,
		),
		a.createHTTPServer(sentryClient, db),
	)

}

// createApprover picks the approver the stage demands.
//
// A stage that requires an approval and has no attention store URL configured
// is a startup failure, not a runtime one: the alternative is a service that
// accepts commands and refuses every one of them for a reason nobody sees until
// the first real deploy.
func (a *application) createApprover(
	ctx context.Context,
	stage apply.Stage,
) (apply.Approver, error) {
	if !stage.RequiresApproval() {
		return apply.NewNoopApprover(), nil
	}
	if a.AttentionURL == "" {
		return nil, errors.Errorf(
			ctx,
			"stage '%s' requires an approval but ATTENTION_URL is empty",
			string(stage),
		)
	}
	if err := validateAttentionURL(ctx, a.AttentionURL); err != nil {
		return nil, err
	}
	return apply.NewAttentionStoreApprover(a.AttentionURL, a.AttentionToken, time.Now), nil
}

// validateAttentionURL refuses a scheme that would put the bearer token on the
// wire in clear text.
//
// Plain http is allowed only for an in-cluster Service address, where the
// traffic never leaves the pod network — the prod store is a ClusterIP reached
// over http, so refusing http outright would make the gate unusable. Anything
// routable must be https.
func validateAttentionURL(ctx context.Context, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse ATTENTION_URL failed")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme != "http" {
		return errors.Errorf(ctx, "ATTENTION_URL must be http or https, got '%s'", parsed.Scheme)
	}
	host := parsed.Hostname()
	if strings.HasSuffix(host, ".svc.cluster.local") ||
		strings.HasSuffix(host, ".svc") ||
		!strings.Contains(host, ".") {
		return nil
	}
	return errors.Errorf(
		ctx,
		"ATTENTION_URL '%s' is plain http to a routable host: the bearer token would travel in clear text — use https",
		raw,
	)
}

func (a *application) createHTTPServer(
	sentryClient libsentry.Client,
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
		router.Path("/testloglevel").Handler(factory.CreateTestLoglevelHandler())
		router.Path("/sentryalert").Handler(factory.CreateSentryAlertHandler(sentryClient))

		glog.V(2).Infof("starting http server listen on %s", a.Listen)
		return libhttp.NewServer(
			a.Listen,
			router,
		).Run(ctx)
	}
}
