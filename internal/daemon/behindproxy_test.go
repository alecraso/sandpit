package daemon

import (
	"net/url"
	"slices"
	"testing"

	"github.com/arugula-salad/sandpit/internal/server"
)

// Behind a proxy the public host is the URL domain, and --url-domain given
// beside it adds domains after it (sprites moved from another host keep their
// names), so the public host stays the default for new sprites.
func TestBehindProxyURLDomains(t *testing.T) {
	u, _ := url.Parse("https://Sprites.Example.com")
	for _, c := range []struct {
		name  string
		flag  string
		extra bool
		want  []string
	}{
		{"alone", "sprites.localhost", false, []string{"sprites.example.com"}},
		{"with --url-domain", "widgets.test,games.test", true, []string{"sprites.example.com", "widgets.test", "games.test"}},
	} {
		f := &Flags{urlDomain: c.flag}
		var opts server.Options
		f.BehindProxy(&opts, u, c.extra)
		got, err := parseURLDomains(f.urlDomain)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: domains %v, %v; want %v", c.name, got, err, c.want)
		}
		if !slices.Equal(opts.APIHosts, []string{"sprites.example.com"}) || !opts.URLsProxied {
			t.Errorf("%s: api hosts %v, proxied %v", c.name, opts.APIHosts, opts.URLsProxied)
		}
	}
}
