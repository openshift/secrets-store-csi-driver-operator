//go:build e2ecoverage

package main

import (
	"log"
	"os"
	"os/signal"
	"runtime/coverage"
	"syscall"
)

func init() {
	if dir := os.Getenv("GOCOVERDIR"); dir != "" {
		os.MkdirAll(dir, 0o777)
	}

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGUSR1)
		for range ch {
			dir := os.Getenv("GOCOVERDIR")
			if dir == "" {
				log.Println("coverage: GOCOVERDIR not set, skipping flush")
				continue
			}
			if err := coverage.WriteCountersDir(dir); err != nil {
				log.Printf("coverage: WriteCountersDir: %v", err)
			}
			if err := coverage.WriteMetaDir(dir); err != nil {
				log.Printf("coverage: WriteMetaDir: %v", err)
			}
			log.Printf("coverage: data flushed to %s", dir)
		}
	}()
}
