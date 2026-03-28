---
name: Bug report
about: Report incorrect crash inspection behavior, output, or docs
title: "[Bug] "
labels: bug
assignees: ''

---

**Describe the bug**
Describe what went wrong.

**To Reproduce**
Share the command you ran and the smallest set of steps needed to reproduce the problem.

```bash
kubectl crashloop POD -n namespace
```

**Expected behavior**
Describe what you expected instead.

**Environment**
- `kubectl-crashloop` version:
- Kubernetes version:
- OS and architecture:
- Install method: Krew, release archive, or `go install`

**Output**
Paste any relevant terminal output, JSON output, or error messages. Redact sensitive details if needed.

**Additional context**
Add any other context that may help, such as whether previous logs were expected, whether Events were present, or whether this was a specific container in a multi-container pod.

If you believe you have found a security issue, please do not file a public bug. Use the private reporting flow in `SECURITY.md` instead.
