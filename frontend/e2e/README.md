# E2E Tests for AgentAPI UI

This directory contains end-to-end tests for the AgentAPI UI application, with a focus on testing authentication functionality using mock backends.

**Note: E2E tests are separate from unit tests and should be run independently.**

## Setup

1. Install Playwright browsers:
```bash
bun run e2e:install
```

2. Run the tests:
```bash
# Run E2E tests in headless mode
bun run e2e

# Run E2E tests with UI mode (interactive)
bun run e2e:ui

# Run E2E tests in headed mode (see the browser)
bun run e2e:headed
```

## Test Separation

- **Unit Tests**: Run with `bun run test` (uses Vitest)
- **E2E Tests**: Run with `bun run e2e` (uses Playwright)

The configurations are completely separate to avoid conflicts.

## Test Structure

- `authentication.spec.ts` - Tests for authentication functionality
- `fixtures/mock-server.ts` - MSW mock server configuration
- `fixtures/test-with-mock.ts` - Custom test fixture with API mocking

## Mock Backend

The tests use MSW (Mock Service Worker) to intercept and mock API requests. This allows us to:
- Test without a real backend
- Control API responses for different scenarios
- Test error handling
- Ensure consistent test results

## Authentication Testing

The tests verify:
- Login page appears when authentication is required
- API key authentication via `/api/auth/login` endpoint
- Cookie-based session management
- Logout functionality that clears the session
- Message sending and receiving when authenticated
- Error handling for invalid API keys

## Environment Variables

The tests use `.env.test` for configuration:
- `COOKIE_ENCRYPTION_SECRET` - Required for cookie encryption in tests