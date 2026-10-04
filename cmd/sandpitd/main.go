// sandpitd is sandpit's daemon: one engine, one store, one set of API keys and
// one dashboard, serving several sandbox providers' APIs so that their official
// SDKs work unmodified. Each API has a listener of its own, and a sandbox
// belongs to the API that created it: E2B sandboxes are not in the Sprites
// API's lists, nor sprites in E2B's, and so on.
//
//		sandpitd --listen 127.0.0.1:7900 --e2b-listen 127.0.0.1:7901 --vercel-listen 127.0.0.1:7902 \
//		  --daytona-listen 127.0.0.1:7903 --modal-listen 127.0.0.1:7904
//
//	  - Sprites (--listen, always on; the dashboard too): SPRITES_API_URL=http://127.0.0.1:7900
//	    and SPRITE_TOKEN (docs/api.md).
//	  - E2B: E2B_API_URL=E2B_SANDBOX_URL=http://127.0.0.1:7901 and E2B_API_KEY (docs/e2b-sdk.md).
//	  - Vercel Sandbox: base URL http://127.0.0.1:7902 and VERCEL_TOKEN (docs/vercel-sdk.md).
//	  - Daytona: DAYTONA_API_URL=http://127.0.0.1:7903/api and DAYTONA_API_KEY (docs/daytona-sdk.md).
//	  - Modal (partial; docs/plans/modal-parity.md): MODAL_SERVER_URL=http://127.0.0.1:7904,
//	    MODAL_TOKEN_SECRET, and any MODAL_TOKEN_ID (docs/modal-client.md).
//
// Every key is the root token (<data>/token) or an API key (sandpitd keys).
// Behind a reverse proxy, --<api>-public-url says where clients reach each one
// (docs/public-urls.md).
//
// With a command instead of flags it is the operator's tool: status, keys and
// images talk to the running daemon over its operator socket; restore and
// backups work offline against the backup bucket.
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/arugula-salad/sandpit/frontend/daytona"
	"github.com/arugula-salad/sandpit/frontend/e2b"
	"github.com/arugula-salad/sandpit/frontend/modal"
	"github.com/arugula-salad/sandpit/frontend/vercel"
	"github.com/arugula-salad/sandpit/internal/confine"
	"github.com/arugula-salad/sandpit/internal/daemon"
)

func main() {
	// The confinement shim: the daemon re-execs itself to put a Landlock
	// domain on a VMM before exec'ing Firecracker (internal/confine).
	if len(os.Args) > 1 && os.Args[1] == confine.ShimArg {
		confine.RunShim(os.Args[2:])
	}
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		switch cmd := os.Args[1]; cmd {
		case "status":
			os.Exit(runStatus(os.Args[2:]))
		case "restore":
			os.Exit(runRestore(os.Args[2:]))
		case "backups":
			os.Exit(runBackups(os.Args[2:]))
		case "images":
			os.Exit(runImages(os.Args[2:]))
		case "keys":
			os.Exit(runKeys(os.Args[2:]))
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q (want status, restore, backups, images or keys; no command runs the daemon)\n", cmd)
			os.Exit(2)
		}
	}

	finish := daemon.Bind(flag.CommandLine)
	e2bListen := flag.String("e2b-listen", "", "serve the E2B API (control plane and sandbox traffic) on this address, e.g. 127.0.0.1:7901; empty (the default) leaves it off")
	e2bDomain := flag.String("e2b-domain", "e2b.localhost", "E2B sandboxes report this as their domain, with --e2b-listen's port appended unless it has one, so the SDK's getHost(port) is <port>-<id>.<domain>, which this listener serves")
	e2bImage := flag.String("e2b-image", "", "the E2B guest disk every E2B sandbox starts from (default <data>/images/e2b.ext4, built by scripts/build-image.sh e2b)")
	e2bMaxTimeout := flag.Duration("e2b-max-timeout", 24*time.Hour, "the longest timeout an E2B sandbox may be given")
	e2bCPUs := flag.Int("e2b-vcpus", 2, "vCPUs per E2B sandbox, as hosted E2B's base template has (0 = --vcpus)")
	e2bMem := flag.Int("e2b-mem-mib", 512, "guest RAM (MiB) per E2B sandbox, as hosted E2B's base template has (0 = --mem-mib)")
	modalListen := flag.String("modal-listen", "", "serve the Modal API (a spike: frontend/modal) on this address, e.g. 127.0.0.1:7904; empty (the default) leaves it off")
	modalImage := flag.String("modal-image", "", "the guest disk every Modal sandbox starts from (default <data>/images/modal.ext4, built by scripts/build-image.sh modal)")
	modalRouter := flag.String("modal-router-url", "", "the URL the Modal client is told to reach the task command router at (default http://<--modal-listen>; the client takes http:// only when MODAL_SERVER_URL is on localhost)")
	vercelListen := flag.String("vercel-listen", "", "serve the Vercel Sandbox API (and its sandboxes' routes) on this address, e.g. 127.0.0.1:7902; empty (the default) leaves it off")
	vercelDomain := flag.String("vercel-domain", "vercel.localhost", "Vercel routes are http://<subdomain>.<domain>, with --vercel-listen's port appended unless it has one, which that listener serves")
	vercelImage := flag.String("vercel-image", "", "the guest disk every Vercel sandbox starts from (default <data>/images/vercel.ext4, built by scripts/build-image.sh vercel)")
	vercelMaxTimeout := flag.Duration("vercel-max-timeout", 24*time.Hour, "the longest a Vercel sandbox session may run")
	vercelMem := flag.Int("vercel-mem-per-vcpu-mib", 2048, "guest RAM (MiB) per vCPU of a Vercel sandbox, as hosted Vercel gives")
	daytonaListen := flag.String("daytona-listen", "", "serve the Daytona API (control plane under /api, toolbox, preview URLs) on this address, e.g. 127.0.0.1:7903; empty (the default) leaves it off")
	daytonaDomain := flag.String("daytona-domain", "daytona.localhost", "Daytona preview URLs are <port>-<id>.<domain>, with --daytona-listen's port appended unless it has one; the domain must resolve to this listener")
	daytonaImage := flag.String("daytona-image", "", "the Daytona guest disk every Daytona sandbox starts from (default <data>/images/daytona.ext4, built by scripts/build-image.sh daytona)")
	daytonaURL := flag.String("daytona-url", "", "how Daytona clients reach --daytona-listen (e.g. https://daytona.example.com), for the toolbox URL sandboxes report; default: --daytona-public-url, else the Host each request came to")
	e2bPublic := flag.String("e2b-public-url", "", "the public URL of --e2b-listen behind a proxy (e.g. https://e2b.example.com): sandboxes report its host as their domain, port and all, instead of --e2b-domain with the listen port")
	vercelPublic := flag.String("vercel-public-url", "", "the public URL of --vercel-listen behind a proxy (e.g. https://vercel.example.com): routes are <scheme>://<subdomain>.<its host>, instead of http:// under --vercel-domain with the listen port")
	spritesPublic := flag.String("sprites-public-url", "", "the public URL of --listen behind a proxy that forwards that host and every name under it (e.g. https://sprites.example.com): the Sprites API is served there as with --api-host, and sprite URLs are <scheme>://<name>.<its host>. With --url-domain too, those domains are served behind the same proxy as well (their sprites keep their names), and this host stays the default for new sprites. Replaces --public-listen")
	daytonaPublic := flag.String("daytona-public-url", "", "the public URL of --daytona-listen behind a proxy (e.g. https://daytona.example.com): previews are <scheme>://<port>-<id>.<its host> and the toolbox is under it, instead of --daytona-domain with the listen port")
	flag.Parse()
	opts, f := finish()
	var pub [3]*url.URL
	for i, p := range []struct{ name, raw string }{{"e2b-public-url", *e2bPublic}, {"vercel-public-url", *vercelPublic}, {"daytona-public-url", *daytonaPublic}} {
		u, err := publicURL(p.raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "--%s: %v\n", p.name, err)
			os.Exit(2)
		}
		pub[i] = u
	}
	if u, err := publicURL(*spritesPublic); err != nil {
		fmt.Fprintf(os.Stderr, "--sprites-public-url: %v\n", err)
		os.Exit(2)
	} else if u != nil {
		if bad := setFlags("public-listen"); bad != "" {
			fmt.Fprintf(os.Stderr, "--sprites-public-url replaces --%s; drop one\n", bad)
			os.Exit(2)
		}
		f.BehindProxy(&opts, u, setFlags("url-domain") != "")
	}
	*e2bDomain = reportedDomain(*e2bDomain, *e2bListen, pub[0])
	*vercelDomain = reportedDomain(*vercelDomain, *vercelListen, pub[1])
	*daytonaDomain = reportedDomain(*daytonaDomain, *daytonaListen, pub[2])
	vercelScheme := "http"
	if pub[1] != nil {
		vercelScheme = pub[1].Scheme
	}
	if *daytonaURL == "" && pub[2] != nil {
		*daytonaURL = pub[2].String()
	}

	daemon.Run("sandpitd", opts, f, modalFrontend(*modalListen, *modalImage, *modalRouter),
		vercelFrontend(*vercelListen, *vercelDomain, vercelScheme, *vercelImage, *vercelMaxTimeout, *vercelMem),
		daytonaFrontend(*daytonaListen, *daytonaDomain, *daytonaImage, *daytonaURL), daemon.Frontend{
			Name:  "the E2B API",
			Addr:  *e2bListen,
			IDLen: 21, // "i" and 20 characters, as hosted E2B's
			Setup: func(env daemon.Env) (http.Handler, error) {
				disk := *e2bImage
				if disk == "" {
					disk = filepath.Join(env.DataDir, "images", "e2b.ext4")
				}
				fe := e2b.New(e2b.Options{Disk: disk, Domain: *e2bDomain, CheckKey: env.Sprites.CheckKey,
					MaxTimeout: *e2bMaxTimeout, CPUs: *e2bCPUs, MemMiB: *e2bMem,
					DefaultCPUs: env.Options.DefaultVCPUs, DefaultMemMiB: env.Options.DefaultMemMiB,
					MaxSandboxes: env.Options.MaxSprites},
					env.Store, env.Engine, env.Log)
				return fe.Handler(), nil
			},
		})
}

// setFlags is the first of names given on the command line, or "".
func setFlags(names ...string) string {
	var set string
	flag.Visit(func(fl *flag.Flag) {
		if set == "" && slices.Contains(names, fl.Name) {
			set = fl.Name
		}
	})
	return set
}

// publicURL parses a --*-public-url: http or https, a host, and nothing
// else, since everything reported under it is built from its host. Empty is
// nil: no proxy in front.
func publicURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("want http(s)://host[:port], got %q", raw)
	}
	if u.User != nil || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("want only a scheme and a host[:port], got %q", raw)
	}
	u.Path = ""
	return u, nil
}

// reportedDomain is the domain an API puts in the URLs it hands its SDK: the
// public URL's host when there is one, port and all (none behind 443);
// otherwise domain, with listen's port appended unless it has one.
func reportedDomain(domain, listen string, public *url.URL) string {
	if public != nil {
		return public.Host
	}
	if _, _, err := net.SplitHostPort(domain); err != nil {
		if _, port, err := net.SplitHostPort(listen); err == nil {
			return net.JoinHostPort(domain, port)
		}
	}
	return domain
}

// modalFrontend is the Modal API (frontend/modal) on addr.
func modalFrontend(addr, image, routerURL string) daemon.Frontend {
	return daemon.Frontend{
		Name:  "the Modal API",
		Addr:  addr,
		IDLen: modal.IDLen,
		Setup: func(env daemon.Env) (http.Handler, error) {
			if image == "" {
				image = filepath.Join(env.DataDir, "images", "modal.ext4")
			}
			if routerURL == "" {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
					host = "127.0.0.1"
				}
				routerURL = "http://" + net.JoinHostPort(host, port)
			}
			fe, err := modal.New(modal.Options{Disk: image, StateFile: filepath.Join(env.DataDir, "modal", "state.json"),
				RouterURL: routerURL, CheckKey: env.Sprites.CheckKey, MaxSandboxes: env.Options.MaxSprites},
				env.Store, env.Engine, env.Log)
			if err != nil {
				return nil, err
			}
			return fe.Handler(), nil
		},
	}
}

// vercelFrontend is the Vercel Sandbox API's listener.
func vercelFrontend(listen, domain, scheme, image string, maxTimeout time.Duration, memPerVCPU int) daemon.Frontend {
	return daemon.Frontend{
		Name: "the Vercel Sandbox API",
		Addr: listen,
		Setup: func(env daemon.Env) (http.Handler, error) {
			if image == "" {
				image = filepath.Join(env.DataDir, "images", "vercel.ext4")
			}
			host, port, _ := net.SplitHostPort(listen)
			fe := vercel.New(vercel.Options{Disk: image, CheckKey: env.Sprites.CheckKey, MaxTimeout: maxTimeout,
				MemPerVCPU: memPerVCPU, MaxSandboxes: env.Options.MaxSprites, RouteURL: func(sub string) string { return scheme + "://" + sub + "." + domain }},
				env.Store, env.Engine, env.Log)
			h := fe.Handler()
			// *.localhost resolves to ::1 where systemd-resolved answers it, so a
			// route on an IPv4 loopback listener is served on the IPv6 one too.
			if host == "127.0.0.1" && strings.HasSuffix(strings.Split(domain, ":")[0], "localhost") {
				if ln, err := net.Listen("tcp", net.JoinHostPort("::1", port)); err == nil {
					go (&http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: h}).Serve(ln)
				} else {
					env.Log.Warn("Vercel routes on *.localhost may not resolve to this listener", "err", err)
				}
			}
			return h, nil
		},
	}
}

// daytonaFrontend is the Daytona API's listener.
func daytonaFrontend(listen, domain, image, baseURL string) daemon.Frontend {
	return daemon.Frontend{
		Name:  "the Daytona API",
		Addr:  listen,
		IDLen: 36, // a UUID, as hosted Daytona's
		Setup: func(env daemon.Env) (http.Handler, error) {
			if image == "" {
				image = filepath.Join(env.DataDir, "images", "daytona.ext4")
			}
			fe := daytona.New(daytona.Options{Disk: image, Domain: domain, BaseURL: baseURL, CheckKey: env.Sprites.CheckKey,
				MaxSandboxes: env.Options.MaxSprites}, env.Store, env.Engine, env.Log)
			return fe.Handler(), nil
		},
	}
}
