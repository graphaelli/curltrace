# Command-line URL Tracer + Go Library

![Example Trace](img/trace.png)

## Command Line Usage

```bash
go run github.com/graphaelli/curltrace/cmd/curltrace $url
```

## Library Usage

Add the library to your project:

```bash
go get github.com/graphaelli/curltrace
```

Update your request creation with some more context:
```go
import "github.com/graphaelli/curltrace"

ctx, done := curltrace.WithClientTrace(context.Background())
req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
client.Do(req)

// process response

done() // signal end of transfer
```

See `cmd/curltrace/curltrace.go` for a full example.
