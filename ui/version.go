package ui

// Version is set at build time via -ldflags "-X claudster/ui.Version=vX.Y.Z".
// Falls back to "dev" when built without ldflags.
var Version = "dev"
