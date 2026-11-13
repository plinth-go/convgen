package controller

import (
	"testing"

	"example.com/proj/internal/api"
	"example.com/proj/internal/db"
)

var benchmarkSink api.Base

func BenchmarkGeneratedConversion(b *testing.B) {
	src := db.Base{ID: 1, Name: "Ada", Age: 37}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkSink = apiBaseFromDB(src)
	}
}

func BenchmarkManualConversion(b *testing.B) {
	src := db.Base{ID: 1, Name: "Ada", Age: 37}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkSink = api.Base{ID: src.ID, Name: src.Name, Age: src.Age}
	}
}
