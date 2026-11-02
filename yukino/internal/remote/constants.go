package remote

// LoopbackHosts are the hosts considered loopback for bind validation,
// mirroring LOOPBACK_HOSTS in src/remote/constants.ts.
var LoopbackHosts = map[string]bool{
	"127.0.0.1": true,
	"::1":       true,
	"localhost": true,
}
