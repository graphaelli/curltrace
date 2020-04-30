package curltrace

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"

	"go.elastic.co/apm"
)

// result stores httpstat info.
type result struct {
	*apm.Transaction

	DNS,
	Connect,
	TLS,
	Server,
	Transfer,
	Total *apm.Span
}

func (r *result) End() {
	if r.Transfer != nil {
		r.Transfer.End()
	}
	if r.Total != nil {
		r.Total.End()
	}
}

func WithClientTrace(ctx context.Context) (context.Context, func()) {
	r := result{
		Transaction: apm.TransactionFromContext(ctx),
	}

	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		DNSStart: func(i httptrace.DNSStartInfo) {
			r.DNS = r.StartSpan("DNS Lookup", "http.dns", nil)
		},

		DNSDone: func(i httptrace.DNSDoneInfo) {
			r.DNS.End()
		},

		ConnectStart: func(_, _ string) {
			r.Connect = r.StartSpan("Connect", "http.connect", nil)

			if r.DNS == nil {
				r.Transaction.Context.SetLabel("dns", false)
			}
		},

		ConnectDone: func(network, addr string, err error) {
			r.Connect.End()
		},

		TLSHandshakeStart: func() {
			r.TLS = r.StartSpan("TLS Handshake", "http.tls", nil)
			r.Transaction.Context.SetLabel("tls", true)
		},

		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			r.TLS.End()
		},

		PutIdleConn: func(err error) {
			r.Transaction.Context.SetLabel("conn_returned", err == nil)
		},

		GotConn: func(i httptrace.GotConnInfo) {
			// Handle when keep alive is used and connection is reused.
			// DNSStart(Done) and ConnectStart(Done) is skipped
			r.Transaction.Context.SetLabel("conn_reused", true)
		},

		WroteRequest: func(info httptrace.WroteRequestInfo) {
			r.Server = r.StartSpan("Server Processing", "http.server", nil)

			// When connection is re-used, DNS/TCP/TLS hooks not called.
			if r.Connect == nil {
				// TODO
			}
		},

		GotFirstResponseByte: func() {
			r.Server.End()
			r.Transfer = r.StartSpan("Transfer", "http.transfer", nil)
		},
	}), r.End
}
