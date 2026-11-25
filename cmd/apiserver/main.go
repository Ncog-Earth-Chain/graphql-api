// Package main implements the API server entry point.
package main

import (
	"net/http"
	"time"

	// REL-05: embed the IANA zone database in the binary so a named location resolves without the
	// alpine tzdata package, which the runtime image no longer installs -- an exact apk pin there
	// expires at every upstream revision, and it had already aged out and broken the build. Nothing
	// in this repository calls time.LoadLocation today; this is here so that staying true does not
	// depend on nobody ever adding such a call.
	_ "time/tzdata"
)

// init initializes the package and sets some important app-wide options
func init() {
	/*
	   Safety net for 'too many open files' issue on legacy code.
	   Set a sane timeout duration for the http.DefaultClient, to ensure idle connections are terminated.
	   Reference: https://stackoverflow.com/questions/37454236/net-http-server-too-many-open-files-error
	*/
	http.DefaultClient.Timeout = time.Second * 10
}

// main initializes the API server and starts it when ready.
func main() {
	app := apiServer{}
	app.init()
	app.run()
}
