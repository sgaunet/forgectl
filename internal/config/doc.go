// Package config loads, merges, and validates forgectl's declared intent: the
// configuration file, the per-repository profile selection, and the three profiles
// built into the binary. Every rule is enforced before any platform call.
package config
