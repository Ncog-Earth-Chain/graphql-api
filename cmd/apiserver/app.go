// Package main implements the API server entry point.
package main

import (
	"flag"
	"log"
	"ncogearthchain-api-graphql/cmd/apiserver/build"
	"ncogearthchain-api-graphql/internal/config"
	"ncogearthchain-api-graphql/internal/graphql/resolvers"
	"ncogearthchain-api-graphql/internal/handlers"
	"ncogearthchain-api-graphql/internal/logger"
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/svc"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// apiServer implements the API server application
type apiServer struct {
	cfg          *config.Config
	log          logger.Logger
	api          resolvers.ApiResolver
	srv          *http.Server
	isVersionReq bool
}

// init initializes the API server
func (app *apiServer) init() {
	// make sure to capture version request and rescan depth
	flag.BoolVar(&app.isVersionReq, "v", false, "get the application version")

	// get the configuration including parsing the calling flags
	var err error
	app.cfg, err = config.Load()
	if nil != err {
		log.Fatal(err)
		return
	}

	// configure logger based on the configuration
	app.log = logger.New(app.cfg)

	// make sure to pass logger and config to internals
	repository.SetConfig(app.cfg)
	repository.SetLogger(app.log)
	resolvers.SetConfig(app.cfg)
	resolvers.SetLogger(app.log)
	svc.SetConfig(app.cfg)
	svc.SetLogger(app.log)

	// make the HTTP server
	app.makeHttpServer()
}

// run executes the API server function.
func (app *apiServer) run() {
	// print the app version and exit if this is the only thing requested
	build.PrintVersion(app.cfg)
	if app.isVersionReq {
		return
	}

	// make sure to capture terminate signals
	app.observeSignals()

	// run services
	svc.Manager().Run()

	// start responding to requests
	app.log.Infof("welcome to NCOGEarthChain GraphQL API server")
	app.log.Infof("listening for requests on %s", app.cfg.Server.BindAddress)

	// listen the interface
	err := app.srv.ListenAndServe()
	if err != nil {
		app.log.Errorf(err.Error())
	}

	// terminate the app
	app.terminate()
}

// makeHttpServer creates and configures the HTTP server to be used to serve incoming requests
func (app *apiServer) makeHttpServer() {
	mux := http.NewServeMux()

	// create HTTP server to handle our requests
	app.srv = &http.Server{
		Addr:              app.cfg.Server.BindAddress,
		ReadTimeout:       time.Second * time.Duration(app.cfg.Server.ReadTimeout),
		WriteTimeout:      time.Second * time.Duration(app.cfg.Server.WriteTimeout),
		IdleTimeout:       time.Second * time.Duration(app.cfg.Server.IdleTimeout),
		ReadHeaderTimeout: time.Second * time.Duration(app.cfg.Server.HeaderTimeout),
		Handler:           mux,
	}

	// setup handlers
	app.setupHandlers(mux)
}

// setupHandlers initializes an array of handlers for our HTTP API end-points.
func (app *apiServer) setupHandlers(mux *http.ServeMux) {
	// create root resolver
	app.api = resolvers.New()

	// Unified GraphQL API handler (both HTTP and WebSocket)
	graphqlApiHandler := handlers.Api(app.cfg, app.log, app.api)

	// HTTP POST GraphQL queries/mutations
	httpHandler := http.TimeoutHandler(
		graphqlApiHandler,
		time.Second*time.Duration(app.cfg.Server.ResolverTimeout),
		"Service timeout.",
	)

	mux.Handle("/api", httpHandler)
	mux.Handle("/graphql", httpHandler)

	// WebSocket Subscriptions
	mux.Handle("/graphql-ws", graphqlApiHandler)

	// setup gas price estimator REST API resolver
	mux.Handle("/json/gas", handlers.GasPrice(app.log))

	// handle GraphiQL interface
	mux.Handle("/graphi", handlers.GraphiHandler(app.cfg.Server.DomainAddress, app.log))
}

// observeSignals setups terminate signals observation.
func (app *apiServer) observeSignals() {
	// log what we do
	app.log.Info("os signals captured")

	// make the signal consumer
	ts := make(chan os.Signal, 1)
	signal.Notify(ts, syscall.SIGINT, syscall.SIGTERM)

	// start monitoring
	go func() {
		// wait for the signal
		<-ts

		// terminate HTTP responder
		app.log.Notice("closing HTTP server")
		if err := app.srv.Close(); err != nil {
			app.log.Errorf("could not terminate HTTP listener")
			os.Exit(0)
		}
	}()
}

// terminate modules of the API server.
func (app *apiServer) terminate() {
	// close resolvers
	app.log.Notice("closing resolver")
	app.api.Close()

	// terminate observers, scanners and dispatchers, etc.
	app.log.Notice("closing services")
	if mgr := svc.Manager(); mgr != nil {
		mgr.Close()
	}

	// terminate connections to DB, blockchain, etc.
	app.log.Notice("closing repository")
	if repo := repository.R(); repo != nil {
		repo.Close()
	}
}
