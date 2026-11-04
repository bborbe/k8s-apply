// Copyright (c) 2025 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	libboltkv "github.com/bborbe/boltkv"
	"github.com/bborbe/cqrs/iam"
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
	"github.com/bborbe/k8s-apply/pkg/promotion"
)

const serviceName = "k8s-apply"

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN           string            `required:"true"  arg:"sentry-dsn"            env:"SENTRY_DSN"            usage:"SentryDSN"                                                                                              display:"length"`
	SentryProxy         string            `required:"false" arg:"sentry-proxy"          env:"SENTRY_PROXY"          usage:"Sentry Proxy"`
	Listen              string            `required:"true"  arg:"listen"                env:"LISTEN"                usage:"address to listen to"`
	KafkaBrokers        string            `required:"true"  arg:"kafka-brokers"         env:"KAFKA_BROKERS"         usage:"Comma separated list of Kafka brokers"`
	BatchSize           int               `required:"true"  arg:"batch-size"            env:"BATCH_SIZE"            usage:"batch consume size"                                                                                                      default:"1"`
	DataDir             string            `required:"true"  arg:"datadir"               env:"DATADIR"               usage:"data directory"`
	Stage               string            `required:"true"  arg:"stage"                 env:"STAGE"                 usage:"stage whose apply commands this instance consumes"                                                                       default:"dev"`
	AttentionURL        string            `required:"false" arg:"attention-url"         env:"ATTENTION_URL"         usage:"attention store base URL, required on a stage that needs an approval"`
	AttentionToken      string            `required:"false" arg:"attention-token"       env:"ATTENTION_TOKEN"       usage:"bearer token for the attention store's gated listener"                                                  display:"length"`
	SunGatewayURL       string            `required:"false" arg:"sun-gateway-url"       env:"SUN_GATEWAY_URL"       usage:"sun command gateway base URL; required on dev, where a healthy apply promotes the component through it"`
	SunGatewayInitiator string            `required:"false" arg:"sun-gateway-initiator" env:"SUN_GATEWAY_INITIATOR" usage:"initiator the sun command gateway attributes the promotion to"                                          display:"length"`
	SunGatewayUser      string            `required:"false" arg:"sun-gateway-user"      env:"SUN_GATEWAY_USER"      usage:"user the sun command gateway authenticates the promotion as; required on dev"`
	SunGatewayPassword  string            `required:"false" arg:"sun-gateway-password"  env:"SUN_GATEWAY_PASSWORD"  usage:"password for the sun command gateway user; required on dev"                                             display:"length"`
	RepoDir             string            `required:"true"  arg:"repo-dir"              env:"REPO_DIR"              usage:"path to the deploy repo checkout this stage applies from"                                                                default:"/repo"`
	Namespace           string            `required:"false" arg:"namespace"             env:"NAMESPACE"             usage:"namespace whose Deployments the post-apply health check reads; defaults to the pod's own"`
	HealthTimeout       time.Duration     `required:"true"  arg:"health-timeout"        env:"HEALTH_TIMEOUT"        usage:"how long a Deployment may take to become healthy after an apply before it is rolled back"                                default:"5m"`
	BuildGitVersion     string            `required:"false" arg:"build-git-version"     env:"BUILD_GIT_VERSION"     usage:"Build Git version"                                                                                                       default:"dev"`
	BuildGitCommit      string            `required:"false" arg:"build-git-commit"      env:"BUILD_GIT_COMMIT"      usage:"Build Git commit hash"                                                                                                   default:"none"`
	BuildDate           *libtime.DateTime `required:"false" arg:"build-date"            env:"BUILD_DATE"            usage:"Build timestamp (RFC3339)"`
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

	gateway, err := a.createGateway(ctx, stage)
	if err != nil {
		return err
	}

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
			a.RepoDir,
			approver,
			deploymentClient,
			a.HealthTimeout,
			gateway,
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
	if err := validateCredentialURL(ctx, "ATTENTION_URL", a.AttentionURL); err != nil {
		return nil, err
	}
	return apply.NewAttentionStoreApprover(a.AttentionURL, a.AttentionToken, time.Now), nil
}

// createGateway picks the promotion gateway the stage demands.
//
// Only a promoting stage gets the real gateway, and only a promoting stage is
// required to configure it: a sun or prod instance has no promotion target, and
// making the URL globally required would crash-loop both. The noop gateway is
// fail-closed — a promotion wired to it is recorded as a failed delivery with a
// reason, never as accepted.
//
// A misconfigured dev instance exits non-zero through service.Main, because Run
// returns this error; the unit tests assert the message, not the process exit.
func (a *application) createGateway(
	ctx context.Context,
	stage apply.Stage,
) (promotion.Gateway, error) {
	if !stage.Promotes() {
		return promotion.NewNoopGateway(), nil
	}
	if a.SunGatewayURL == "" {
		return nil, errors.Errorf(
			ctx,
			"stage '%s' promotes but SUN_GATEWAY_URL is empty",
			string(stage),
		)
	}
	if err := validateCredentialURL(ctx, "SUN_GATEWAY_URL", a.SunGatewayURL); err != nil {
		return nil, err
	}
	if a.SunGatewayInitiator == "" {
		return nil, errors.Errorf(
			ctx,
			"stage '%s' promotes but SUN_GATEWAY_INITIATOR is empty",
			string(stage),
		)
	}
	// The initiator header is not an authentication mechanism: sun's gateway
	// answers an unauthenticated command with a 307 to Google OAuth, measured
	// 2026-11-04. The credential is what gets the request through; the header is
	// what the gateway attributes it to.
	if a.SunGatewayUser == "" {
		return nil, errors.Errorf(
			ctx,
			"stage '%s' promotes but SUN_GATEWAY_USER is empty",
			string(stage),
		)
	}
	if a.SunGatewayPassword == "" {
		return nil, errors.Errorf(
			ctx,
			"stage '%s' promotes but SUN_GATEWAY_PASSWORD is empty",
			string(stage),
		)
	}
	return promotion.NewGateway(
		a.SunGatewayURL,
		iam.Initiator(a.SunGatewayInitiator),
		a.SunGatewayUser,
		a.SunGatewayPassword,
		&http.Client{Timeout: 10 * time.Second},
	), nil
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

// validateCredentialURL refuses a scheme that would put a credential on the
// wire in clear text.
//
// Plain http is allowed only for an in-cluster Service address, where the
// traffic never leaves the pod network — the prod store is a ClusterIP reached
// over http, so refusing http outright would make the gate unusable. Anything
// routable must be https.
//
// The rule is shared because both settings carry a credential: the attention
// token in an Authorization header, and the sun gateway's initiator identity.
func validateCredentialURL(ctx context.Context, setting string, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse %s failed", setting)
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme != "http" {
		return errors.Errorf(ctx, "%s must be http or https, got '%s'", setting, parsed.Scheme)
	}
	host := parsed.Hostname()
	if strings.HasSuffix(host, ".svc.cluster.local") ||
		strings.HasSuffix(host, ".svc") ||
		!strings.Contains(host, ".") {
		return nil
	}
	return errors.Errorf(
		ctx,
		"%s '%s' is plain http to a routable host: the credential would travel in clear text — use https",
		setting,
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
