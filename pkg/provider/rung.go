package provider

import "strings"

// ParseRung resolves an escalation-ladder entry into a (provider, model)
// pair. Entries are either a bare model name (runs on defaultProvider) or
// "provider/model" where the prefix before the first slash names a
// registered provider. An unregistered prefix is treated as part of the
// model name, so entries like "hf.co/some-model" work on the default
// provider.
func ParseRung(entry, defaultProvider string) (name, model string) {
	if prefix, rest, found := strings.Cut(entry, "/"); found {
		if _, err := Get(prefix); err == nil {
			return prefix, rest
		}
	}
	return defaultProvider, entry
}
