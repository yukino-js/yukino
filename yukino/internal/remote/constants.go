package remote

// LoopbackHosts are the hosts considered loopback for bind validation,
// mirroring LOOPBACK_HOSTS in src/remote/constants.ts.
var LoopbackHosts = map[string]bool{
	"127.0.0.1": true,
	"::1":       true,
	"localhost": true,
}

// TextPreviewChars caps the text preview attached to structured event logs.
const TextPreviewChars = 800

// PendingRequestTimeoutMS settles permission/ask requests with no client response.
const PendingRequestTimeoutMS = 10 * 60 * 1000
