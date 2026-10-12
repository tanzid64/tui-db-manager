package buildinfo

// Version is overridden at release time via -ldflags "-X .../buildinfo.Version=...".
var Version = "dev"
