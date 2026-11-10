package feishulogin

import (
	"net/url"
	"strings"
)

// FormRedirectPolicy permits only origins of server-validated OAuth destinations.
// Chromium checks form-action on redirect hops as well as the form POST itself.
// Callers must not pass destinations from unvalidated request parameters.
func FormRedirectPolicy(policy string, destinations ...string) string {
	sources := "form-action 'self'"
	for _, destination := range destinations {
		u, err := url.Parse(destination)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || strings.ContainsAny(u.Host, " ;'\"\t\r\n*\\") {
			continue
		}
		source := u.Scheme + "://" + u.Host
		if !strings.Contains(sources, " "+source) {
			sources += " " + source
		}
	}
	return strings.Replace(policy, "form-action 'self'", sources, 1)
}
