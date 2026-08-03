// Package mongo_tls owns the Mongo TLS PEM material lifecycle — writing the
// temporary combined PEM used for mongodump/mongorestore --tlsCAFile-style
// arguments, a process-wide registry for signal-handler cleanup, and startup
// orphan sweep. It is a neutral home imported symmetrically
// by the dump and restore Mongo adapters and by CLI startup/shutdown, so no
// engine adapter owns it and no adapter reaches across axes.
package mongo_tls
