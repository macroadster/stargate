package starlight

// This file previously contained the deprecated StarlightScanner implementation.
// The StarlightScanner has been removed in favor of the unified ScannerManager
// which provides circuit breaker protection and proper error handling.
//
// Use GetScannerManager() to access the scanner functionality:
//   manager := starlight.GetScannerManager()
//   manager.InitializeScanner()
//   result, err := manager.ScanImage(imageData, options)
//
// The manager automatically handles fallback to mock scanner when the Python API
// is unavailable and implements circuit breaker pattern for resilience.
