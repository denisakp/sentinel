// Package storagetesting provides an in-memory StorageBackend implementation
// and shared helpers for the storage contract test suite.
//
// Relocated from internal/storage/storagetesting/ so that domain and driver
// test code can obtain a ports.StorageBackend fake without importing any
// concrete adapter under internal/adapters/storage/*.
package storagetesting
