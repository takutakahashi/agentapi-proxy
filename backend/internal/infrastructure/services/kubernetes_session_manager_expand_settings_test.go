package services

// The settings expansion / merge tests have been migrated to pkg/settingspatch.
// The behaviors previously tested by TestExpandSettingsToEnv_* are now covered by:
//   - TestMaterialize_AuthMode                         (materialize_test.go)
//   - TestResolve/auth_mode_nil_does_not_override...   (apply_test.go)
//   - TestMaterialize_FullPipeline                     (materialize_test.go)
//
// This file is intentionally empty. Delete it or add service-layer integration
// tests here if needed.
