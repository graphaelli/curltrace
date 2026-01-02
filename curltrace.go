package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type stringSlice []string

func (s *stringSlice) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func initTracer() (*sdktrace.TracerProvider, error) {
	client := otlptracehttp.NewClient()
	exporter, err := otlptrace.New(context.Background(), client)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(semconv.ServiceNameKey.String("curltrace")),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp, nil
}

func main() {
	tp, err := initTracer()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	// Curl-like options
	method := flag.String("X", "GET", "HTTP method (e.g., GET, POST, PUT, DELETE)")
	var headers stringSlice
	flag.Var(&headers, "H", "HTTP header (can be used multiple times)")
	data := flag.String("d", "", "HTTP request body data")
	verbose := flag.Bool("v", false, "Verbose output")
	includeHeaders := flag.Bool("i", false, "Include response headers in output")
	followRedirects := flag.Bool("L", true, "Follow redirects")

	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		log.Fatal("Error: URL is required as a positional argument")
	}
	url := args[0]

	httpMethod := *method
	allHeaders := headers
	requestBody := *data

	isVerbose := *verbose
	includeRespHeaders := *includeHeaders
	followRedirectsFlag := *followRedirects

	client := http.Client{
		Transport: otelhttp.NewTransport(
			http.DefaultTransport,
			otelhttp.WithClientTrace(func(ctx context.Context) *httptrace.ClientTrace {
				return otelhttptrace.NewClientTrace(ctx)
			}),
		),
	}
	if !followRedirectsFlag {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	ctx := context.Background()
	var body []byte
	var traceID trace.TraceID

	tr := otel.Tracer("example/client")
	err = func(ctx context.Context) error {
		ctx, span := tr.Start(ctx, fmt.Sprintf("curl %s", url), trace.WithAttributes(semconv.PeerService("Remote HTTP Service")))
		defer span.End()
		traceID = span.SpanContext().TraceID()

		var bodyReader io.Reader
		if requestBody != "" {
			bodyReader = strings.NewReader(requestBody)
		} else {
			bodyReader = http.NoBody
		}

		req, _ := http.NewRequestWithContext(ctx, httpMethod, url, bodyReader)
		for _, h := range allHeaders {
			parts := strings.SplitN(h, ":", 2)
			if len(parts) == 2 {
				req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}

		if isVerbose {
			fmt.Printf("Sending %s request to %s\n", httpMethod, url)
			if len(allHeaders) > 0 {
				fmt.Printf("Headers:\n")
				for _, h := range allHeaders {
					fmt.Printf("  %s\n", h)
				}
			}
			if requestBody != "" {
				fmt.Printf("Body: %s\n", requestBody)
			}
		} else {
			fmt.Printf("Sending request...\n")
		}

		res, err := client.Do(req)
		if err != nil {
			panic(err)
		}

		if includeRespHeaders {
			fmt.Printf("HTTP/1.1 %s\n", res.Status)
			for k, v := range res.Header {
				for _, val := range v {
					fmt.Printf("%s: %s\n", k, val)
				}
			}
			fmt.Printf("\n")
		}

		body, err = io.ReadAll(res.Body)
		_ = res.Body.Close()

		return err
	}(ctx)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s\n\n\n", body)
	log.Printf("Trace ID: %s\n", traceID.String())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := tp.ForceFlush(ctx); err != nil {
		log.Printf("Error flushing spans: %v", err)
	}
}
