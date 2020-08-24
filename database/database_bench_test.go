package database

import (
	"runtime"
	"testing"

	"github.com/System-Glitch/goyave/v3/config"
)

func setupDatabaseBench(b *testing.B) {
	if err := config.LoadFrom("config.test.json"); err != nil {
		panic(err)
	}
	runtime.GC()
	b.ReportAllocs()
	b.ResetTimer()
}

func BenchmarkBuildConnectionOptions(b *testing.B) {
	setupDatabaseBench(b)
	for n := 0; n < b.N; n++ {
		buildConnectionOptions("mysql")
	}
}
