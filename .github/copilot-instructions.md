# GitHub Copilot Code Review Instructions

## Review Philosophy

- Only comment when you have HIGH CONFIDENCE (>80%) that an issue exists
- Be concise
- Focus on actionable feedback, not observations
- If you're uncertain, stay silent—false positives reduce trust

## Project Context

Kubernetes Operator for PostgreSQL (Operator SDK, controller-runtime). Go + YAML. Key paths: `internal/`, `percona/`, `pkg/apis` and `e2e-tests/`.

## Priority Areas

### Security

- Hardcoded secrets, credentials, or API keys
- SQL injection—use parameterized queries, never string concatenation
- Missing or overly broad RBAC (`+kubebuilder:rbac` on reconcile functions)
- Logging of secrets or sensitive data
- Unvalidated user input before DB operations

### Correctness

- Logic errors that could cause panics or incorrect behavior
- Race conditions, resource leaks (files, connections, memory)
- Incorrect or missing error propagation
- Error wrapping that doesn't add useful context
- Redundant comments that restate what the code shows

### Imports and Dependencies

- Use standard import aliases: `corev1`, `appsv1`, `metav1`, `apierrors`, etc. (per `.golangci.yaml`)
- Import order: standard, default, `github.com/percona` prefix

### Controller / Reconcile Logic

- Add `+kubebuilder:rbac` above reconcile functions that create/update K8s resources
- Set controller/owner references for owned resources
- Idempotent reconcile; handle `apierrors.IsConflict` with requeue

### Logging

- Prefer `logging.FromContext(ctx)` for loggers
- Use structured fields: `log.Info("message", "key", value)`
- Add logging for important operator actions (reconcile steps, errors, retries)

### Testing

- New features: expect unit tests and/or E2E (KUTTL) where appropriate
- Unit tests should use `assert` and `require` from `github.com/stretchr/testify` wherever applicable
- Utilize table driven tests when possible
- Test names should describe the scenario

## Response Format

When you identify an issue:

1. **Problem** (1 sentence)
2. **Why it matters** (1 sentence, only if not obvious)
3. **Fix** (concrete suggestion or code snippet)

Example:
```
1. **Problem**: This map access can panic if the map is nil.
2. **Why it matters**: A panic can crash the operator and disrupt reconciliation.
3. **Fix**: Initialize the map before use, e.g. `m := make(map[string]string)` before assigning or reading.
```

## When to Stay Silent

- You're uncertain whether something is an issue
- The concern is stylistic and the code is acceptable
- The fix would be a matter of preference, not correctness or security
