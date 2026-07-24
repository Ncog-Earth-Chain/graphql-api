// Command migrate applies the explorer's PostgreSQL schema migrations against the database
// named by -dsn (or, if that is empty, the PG_URL then EXPLORER_TEST_DSN environment
// variables).
//
// It runs the SAME embedded migrations, through the SAME migrate.Up entry point, that the
// API server applies at startup. CI and operators therefore exercise the production
// migration path rather than a separately installed tool that might interpret the goose
// annotations differently.
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"ncogearthchain-api-graphql/internal/repository/db/migrate"
)

func main() {
	dsn := flag.String("dsn", "", "PostgreSQL DSN; defaults to $PG_URL then $EXPLORER_TEST_DSN")
	flag.Parse()

	target := *dsn
	if target == "" {
		target = firstNonEmpty(os.Getenv("PG_URL"), os.Getenv("EXPLORER_TEST_DSN"))
	}
	if target == "" {
		log.Fatal("no DSN: pass -dsn or set PG_URL / EXPLORER_TEST_DSN")
	}

	if err := migrate.Up(context.Background(), target, stdLogger{}); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
	log.Println("migrations applied")
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// stdLogger is a minimal logger.Logger backed by the standard library logger. The migrate
// package emits only a handful of leveled lines, so nothing richer is needed here.
type stdLogger struct{}

func (stdLogger) Fatal(a ...interface{})            { log.Fatal(a...) }
func (stdLogger) Fatalf(f string, a ...interface{}) { log.Fatalf(f, a...) }
func (stdLogger) Panic(a ...interface{})            { log.Panic(a...) }
func (stdLogger) Panicf(f string, a ...interface{}) { log.Panicf(f, a...) }
func (stdLogger) Critical(a ...interface{})            { log.Print(a...) }
func (stdLogger) Criticalf(f string, a ...interface{}) { log.Printf(f, a...) }
func (stdLogger) Error(a ...interface{})            { log.Print(a...) }
func (stdLogger) Errorf(f string, a ...interface{}) { log.Printf(f, a...) }
func (stdLogger) Warning(a ...interface{})            { log.Print(a...) }
func (stdLogger) Warningf(f string, a ...interface{}) { log.Printf(f, a...) }
func (stdLogger) Notice(a ...interface{})            { log.Print(a...) }
func (stdLogger) Noticef(f string, a ...interface{}) { log.Printf(f, a...) }
func (stdLogger) Info(a ...interface{})            { log.Print(a...) }
func (stdLogger) Infof(f string, a ...interface{}) { log.Printf(f, a...) }
func (stdLogger) Debug(a ...interface{})            { log.Print(a...) }
func (stdLogger) Debugf(f string, a ...interface{}) { log.Printf(f, a...) }
func (stdLogger) Printf(f string, a ...interface{}) { log.Printf(f, a...) }
