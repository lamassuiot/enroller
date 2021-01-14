package main

import (
	"enroller/pkg/ca/api"
	"enroller/pkg/ca/auth"
	"enroller/pkg/ca/configs"
	"enroller/pkg/ca/discovery/consul"
	"enroller/pkg/ca/secrets/vault"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	kitprometheus "github.com/go-kit/kit/metrics/prometheus"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	var logger log.Logger
	{
		logger = log.NewJSONLogger(os.Stdout)
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
		logger = level.NewFilter(logger, level.AllowInfo())
	}

	cfg, err := configs.NewConfig("ca")
	if err != nil {
		level.Error(logger).Log("err", err, "msg", "Could not read environment configuration values")
		os.Exit(1)
	}
	level.Info(logger).Log("msg", "Environment configuration values loaded")

	auth := auth.NewAuth(cfg.KeycloakHostname, cfg.KeycloakPort, cfg.KeycloakProtocol, cfg.KeycloakRealm, cfg.KeycloakCA)
	level.Info(logger).Log("msg", "Connection established with authentication system")

	secrets, err := vault.NewVaultSecrets(cfg.VaultAddress, cfg.VaultRoleID, cfg.VaultSecretID, cfg.VaultCA, logger)
	if err != nil {
		level.Error(logger).Log("err", err, "msg", "Could not start connection with Vault Secret Engine")
		os.Exit(1)
	}
	level.Info(logger).Log("msg", "Connection established with secret engine")

	fieldKeys := []string{"method"}

	var s api.Service
	{
		s = api.NewCAService(secrets)
		s = api.LoggingMiddleware(logger)(s)
		s = api.NewInstrumentingMiddleware(
			kitprometheus.NewCounterFrom(stdprometheus.CounterOpts{
				Namespace: "enroller",
				Subsystem: "enroller_service",
				Name:      "request_count",
				Help:      "Number of requests received.",
			}, fieldKeys),
			kitprometheus.NewSummaryFrom(stdprometheus.SummaryOpts{
				Namespace: "enroller",
				Subsystem: "enroller_service",
				Name:      "request_latency_microseconds",
				Help:      "Total duration of requests in microseconds.",
			}, fieldKeys),
		)(s)
	}

	consulsd, err := consul.NewServiceDiscovery(cfg.ConsulProtocol, cfg.ConsulHost, cfg.ConsulPort, logger)
	if err != nil {
		level.Error(logger).Log("err", err, "msg", "Could not start connection with Consul Service Discovery")
		os.Exit(1)
	}
	level.Info(logger).Log("msg", "Connection established with Consul Service Discovery")

	mux := http.NewServeMux()

	mux.Handle("/v1/", api.MakeHTTPHandler(s, log.With(logger, "component", "HTTPS"), auth))
	http.Handle("/", accessControl(mux, cfg.EnrollerUIProtocol, cfg.EnrollerUIHost, cfg.EnrollerUIPort))
	http.Handle("/metrics", promhttp.Handler())

	errs := make(chan error)
	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errs <- fmt.Errorf("%s", <-c)
	}()

	go func() {
		level.Info(logger).Log("transport", "HTTPS", "address", ":"+cfg.Port, "msg", "listening")
		consulsd.Register("https", "ca", cfg.Port)
		errs <- http.ListenAndServeTLS(":"+cfg.Port, cfg.CertFile, cfg.KeyFile, nil)
	}()

	level.Info(logger).Log("exit", <-errs)
	consulsd.Deregister()

}

func accessControl(h http.Handler, enrollerUIProtocol string, enrollerUIHost string, enrollerUIPort string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", enrollerUIProtocol+"://"+enrollerUIHost+":"+enrollerUIPort)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			return
		}

		h.ServeHTTP(w, r)
	})
}
