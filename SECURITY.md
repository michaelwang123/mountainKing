# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |

## Reporting a Vulnerability

If you discover a security vulnerability in MountainKing, please report it responsibly:

1. **Do NOT** open a public GitHub issue for security vulnerabilities.
2. Email the maintainer directly or use [GitHub Security Advisories](https://github.com/michaelwang123/mountainKing/security/advisories/new).
3. Include a description of the vulnerability, steps to reproduce, and potential impact.
4. You will receive an acknowledgment within 72 hours.

## Security Features

This project includes built-in security measures:
- JWT/API Key authentication with bcrypt hashing
- Role-based access control (RBAC) per datasource
- SQL injection prevention (parameterized queries + lexer-based validation)
- Rate limiting (local + distributed with automatic fallback)
- CSRF protection, CORS, request body size limits
- Authentication failure brute-force protection
- Sensitive data sanitization in logs and traces
- CDN resources with SRI integrity hashes (marked.js, highlight.js, DOMPurify)
