package update

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// DefaultCustomAddress supplies this fork's release source until a user chooses their own.
const DefaultCustomAddress = "https://github.com/zhaoshihang/motty"

var githubName = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// CustomFeed accepts a GitHub repository or a self-hosted Motty mirror manifest.
// GitHub returns a release page. Only a signed mirror asset can be installed in place.
func CustomFeed(address string) (feed string, github bool, err error) {
	invalid := errors.New("enter a GitHub repository or an HTTPS Motty release feed")
	raw := strings.TrimSpace(address)
	if !strings.Contains(raw, "://") {
		if strings.Count(raw, "/") != 1 {
			return "", false, invalid
		}
		raw = "https://github.com/" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return "", false, invalid
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	switch strings.ToLower(u.Hostname()) {
	case "github.com", "www.github.com":
		switch {
		case len(parts) == 2:
		case len(parts) == 3 && parts[2] == "releases":
		case len(parts) == 4 && parts[2] == "releases" && parts[3] == "latest":
		default:
			return "", false, invalid
		}
		parts = parts[:2]
	case "api.github.com":
		if len(parts) != 5 || parts[0] != "repos" || parts[3] != "releases" || parts[4] != "latest" {
			return "", false, invalid
		}
		parts = parts[1:3]
	default:
		return u.String(), false, nil
	}
	parts[1] = strings.TrimSuffix(parts[1], ".git")
	for _, part := range parts {
		if !githubName.MatchString(part) || part == "." || part == ".." {
			return "", false, invalid
		}
	}
	if u.Port() != "" && u.Port() != "443" {
		return "", false, invalid
	}
	return "https://api.github.com/repos/" + parts[0] + "/" + parts[1] + "/releases/latest", true, nil
}
