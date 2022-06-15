package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.10.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer         trace.Tracer
	serviceName    string = "test-go-server"
	serviceVersion string = "0.1.0"
	collectorAddr  string = "localhost:4318" // HTTP endpoint for collector
)

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

var src = rand.NewSource(time.Now().UnixNano())

func newTraceProvider(ctx context.Context) *sdktrace.TracerProvider {
	exporter, err :=
		otlptracehttp.New(ctx,
			// WithInsecure lets us use http instead of https.
			// This is just for local development.
			otlptracehttp.WithInsecure(),
			otlptracehttp.WithEndpoint(collectorAddr),
		)

	if err != nil {
		panic(err)
	}

	// This includes the following resources:
	//
	// - sdk.language, sdk.version
	// - service.name, service.version, environment
	//
	// Including these resources is a good practice because it is commonly
	// used by various tracing backends to let you more accurately
	// analyze your telemetry data.
	resource, rErr :=
		resource.Merge(
			resource.Default(),
			resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String(serviceName),
				semconv.ServiceVersionKey.String(serviceVersion),
				attribute.String("environment", "getting-started"),
			),
		)

	if rErr != nil {
		panic(rErr)
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource),
	)
}

func randString(n int) string {
	sb := strings.Builder{}
	sb.Grow(n)
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			sb.WriteByte(letterBytes[idx])
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return sb.String()
}

// Wrap the handleRollDice so that telemetry data
// can be automatically generated for it
func wrapHandler() {
	handler := http.HandlerFunc(handlePing)
	wrappedHandler := otelhttp.NewHandler(handler, "ping")
	http.Handle("/ping", wrappedHandler)
}

func handlePing(w http.ResponseWriter, r *http.Request) {
	operationName := "ping"
	_, span := tracer.Start(r.Context(), operationName)
	defer span.End()

	length := rand.Intn(1024)
	log.Printf("%s %s %s", r.Method, r.URL.Path, r.Proto)

	pingResult := randString(length)
	span.SetAttributes(
		attribute.String("result", pingResult),
		attribute.String("library.language", "go"),
		attribute.String("library.version", "v1.7.0"),
	)

	// setting span as error
	span.SetStatus(codes.Ok, "Success")

	// setting span event
	span.AddEvent(fmt.Sprint(r.Header))

	fmt.Fprintf(w, pingResult)
}

func main() {
	ctx := context.Background()

	tp := newTraceProvider(ctx)
	defer func() { _ = tp.Shutdown(ctx) }()

	otel.SetTracerProvider(tp)

	// Register context and baggage propagation.
	// Although not strictly necessary, for this sample,
	// it is required for distributed tracing.
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	tracer = tp.Tracer(serviceName)

	wrapHandler()

	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		log.Fatal(err)
	}
}
