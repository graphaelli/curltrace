package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"go.elastic.co/apm"
	"go.elastic.co/apm/module/apmhttp"
)

func flush(tracer *apm.Tracer) {
	ctxFlush, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		tracer.Flush(ctxFlush.Done())
	}()
	for {
		select {
		case <-time.After(50 * time.Millisecond):
			log.Println("Waiting for transaction to be flushed...")
		case <-flushed:
			return
		}
	}
}

func main() {
	kibana := flag.String("k", "http://localhost:5601", "kibana base path")
	flag.Parse()
	url := flag.Arg(0)
	output := os.Stdout

	client := http.DefaultClient
	client.Transport = apmhttp.WrapRoundTripper(client.Transport, apmhttp.WithClientTrace())

	tx := apm.DefaultTracer.StartTransaction("GET "+url, "request")
	ctx := apm.ContextWithTransaction(context.Background(), tx)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	rsp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer rsp.Body.Close()
	fmt.Println(rsp.Proto, rsp.Status)
	io.Copy(output, rsp.Body)
	fmt.Println()

	apmhttp.SetTransactionContext(tx, req, &apmhttp.Response{
		StatusCode: rsp.StatusCode,
		Headers:    rsp.Header,
	}, nil)

	traceContext := tx.TraceContext()
	tx.End()
	flush(apm.DefaultTracer)
	log.Printf("%s/app/apm#/link-to/trace/%s\n", *kibana, traceContext.Trace.String())
}
