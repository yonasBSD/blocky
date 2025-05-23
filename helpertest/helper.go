package helpertest

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/0xERR0R/blocky/log"
	"github.com/0xERR0R/blocky/model"

	"github.com/miekg/dns"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

const (
	A     = dns.Type(dns.TypeA)
	AAAA  = dns.Type(dns.TypeAAAA)
	CNAME = dns.Type(dns.TypeCNAME)
	HTTPS = dns.Type(dns.TypeHTTPS)
	MX    = dns.Type(dns.TypeMX)
	PTR   = dns.Type(dns.TypePTR)
	SRV   = dns.Type(dns.TypeSRV)
	TXT   = dns.Type(dns.TypeTXT)
	DS    = dns.Type(dns.TypeDS)
)

// GetIntPort returns a port for the current testing
// process by adding the current ginkgo parallel process to
// the base port and returning it as int.
func GetIntPort(port int) int {
	return port + ginkgo.GinkgoParallelProcess()
}

// GetStringPort returns a port for the current testing
// process by adding the current ginkgo parallel process to
// the base port and returning it as string.
func GetStringPort(port int) string {
	return fmt.Sprintf("%d", GetIntPort(port))
}

// GetHostPort returns a host:port string for the current testing
// process by adding the current ginkgo parallel process to
// the base port and returning it as string.
func GetHostPort(host string, port int) string {
	return net.JoinHostPort(host, GetStringPort(port))
}

// TempFile creates temp file with passed data
func TempFile(data string) *os.File {
	f, err := os.CreateTemp("", "prefix")
	if err != nil {
		log.Log().Fatal(err)
	}

	_, err = f.WriteString(data)
	if err != nil {
		log.Log().Fatal(err)
	}

	return f
}

// TestServer creates temp http server with passed data
func TestServer(data string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		_, err := rw.Write([]byte(data))
		if err != nil {
			log.Log().Fatal("can't write to buffer:", err)
		}
	}))

	ginkgo.DeferCleanup(srv.Close)

	return srv
}

// DoGetRequest performs a GET request
func DoGetRequest(ctx context.Context, url string,
	fn func(w http.ResponseWriter, r *http.Request),
) (*httptest.ResponseRecorder, *bytes.Buffer) {
	r, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(fn)

	handler.ServeHTTP(rr, r)

	return rr, rr.Body
}

func ToAnswer(m *model.Response) []dns.RR {
	return m.Res.Answer
}

func ToExtra(m *model.Response) []dns.RR {
	return m.Res.Extra
}

func HaveNoAnswer() types.GomegaMatcher {
	return gomega.WithTransform(ToAnswer, gomega.BeEmpty())
}

func HaveReason(reason string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(m *model.Response) (bool, error) {
		return m.Reason == reason, nil
	}).WithTemplate(
		"Expected:\n{{.Actual}}\n{{.To}} have reason:\n{{format .Data 1}}",
		reason,
	)
}

func HaveResponseType(c model.ResponseType) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(m *model.Response) (bool, error) {
		return m.RType == c, nil
	}).WithTemplate(
		"Expected:\n{{.Actual}}\n{{.To}} have ResponseType:\n{{format .Data 1}}",
		c.String(),
	)
}

func HaveReturnCode(code int) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(m *model.Response) (bool, error) {
		return m.Res.Rcode == code, nil
	}).WithTemplate(
		"Expected:\n{{.Actual}}\n{{.To}} have RCode:\n{{format .Data 1}}",
		fmt.Sprintf("%d (%s)", code, dns.RcodeToString[code]),
	)
}

// HaveEdnsOption checks if the given message contains an EDNS0 record with the given option code.
func HaveEdnsOption(code uint16) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual any) (bool, error) {
		var opt *dns.OPT
		switch msg := actual.(type) {
		case *model.Response:
			opt = msg.Res.IsEdns0()
		case *dns.Msg:
			opt = msg.IsEdns0()
		}

		if opt != nil {
			for _, o := range opt.Option {
				if o.Option() == code {
					return true, nil
				}
			}
		}

		return false, nil
	}).WithTemplate(
		"Expected:\n{{.Actual}}\n{{.To}} have EDNS option:\n{{format .Data 1}}",
		code,
	)
}

func HaveTTL(matcher types.GomegaMatcher) types.GomegaMatcher {
	return gomega.WithTransform(func(actual interface{}) (uint32, error) {
		// Handle different types of input
		var records []dns.RR

		switch i := actual.(type) {
		case *model.Response:
			records = i.Res.Answer
		case *dns.Msg:
			records = i.Answer
		case []dns.RR:
			records = i
		case dns.RR:
			records = []dns.RR{i}
		default:
			return 0, fmt.Errorf("unsupported type for TTL matching: %T", actual)
		}

		// No records to match
		if len(records) == 0 {
			return 0, fmt.Errorf("answer must not be empty")
		}

		// Return TTL of the first record
		// This is a reasonable approach since typically all records in a response
		// have the same TTL, and we're usually testing against a specific expected value
		return records[0].Header().Ttl, nil
	}, matcher)
}

// BeDNSRecord returns new dns matcher
func BeDNSRecord(domain string, dnsType dns.Type, answer string) types.GomegaMatcher {
	return &dnsRecordMatcher{
		domain:  domain,
		dnsType: dnsType,
		answer:  answer,
	}
}

type dnsRecordMatcher struct {
	domain  string
	dnsType dns.Type
	answer  string
}

func (matcher *dnsRecordMatcher) matchSingle(rr dns.RR) bool {
	if (rr.Header().Name != matcher.domain) ||
		(dns.Type(rr.Header().Rrtype) != matcher.dnsType) {
		return false
	}

	switch v := rr.(type) {
	case *dns.A:
		return v.A.String() == matcher.answer
	case *dns.AAAA:
		return v.AAAA.To16().Equal(net.ParseIP(matcher.answer))
	case *dns.CNAME:
		return v.Target == matcher.answer
	case *dns.PTR:
		return v.Ptr == matcher.answer
	case *dns.SRV:
		return fmt.Sprintf("%d %d %d %s", v.Priority, v.Weight, v.Port, v.Target) == matcher.answer
	case *dns.TXT:
		return strings.Join(v.Txt, " ") == matcher.answer
	case *dns.MX:
		return v.Mx == matcher.answer
	}

	return false
}

// Match checks the DNS record
func (matcher *dnsRecordMatcher) Match(actual interface{}) (success bool, err error) {
	// Handle different types of input
	var records []dns.RR

	switch i := actual.(type) {
	case *model.Response:
		records = i.Res.Answer
	case *dns.Msg:
		records = i.Answer
	case []dns.RR:
		records = i
	case dns.RR:
		records = []dns.RR{i}
	default:
		return false, fmt.Errorf("unsupported type for DNS record matching: %T", actual)
	}

	// No records to match
	if len(records) == 0 {
		return false, nil
	}

	// Try to match any of the records
	for _, rr := range records {
		if match := matcher.matchSingle(rr); match {
			return true, nil
		}
	}

	return false, nil
}

// FailureMessage generates a failure message
func (matcher *dnsRecordMatcher) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected\n\t%s\n to contain\n\t domain '%s', type '%s', answer '%s'",
		actual, matcher.domain, dns.TypeToString[uint16(matcher.dnsType)], matcher.answer)
}

// NegatedFailureMessage creates negated message
func (matcher *dnsRecordMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected\n\t%s\n not to contain\n\t domain '%s', type '%s', answer '%s'",
		actual, matcher.domain, dns.TypeToString[uint16(matcher.dnsType)], matcher.answer)
}
