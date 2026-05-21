# database_scan GUI Design Rules

This GUI is a database sensitive-information audit workstation. It must not look like a generic SaaS dashboard, marketing page, or AI-generated admin template.

## Product Feel

- Dense, quiet, evidence-first security tooling.
- The first screen must expose target connection, scan settings, table findings, sample rows, logs, and errors.
- The interface should feel closer to a database audit console than a landing page.

## Layout

- Use a three-column workstation layout plus a bottom console.
- Left rail: connection information, authentication and scan parameters, action controls.
- Center stage: progress, database scan queue, sensitive field/table findings.
- Right rail: selected-table evidence, field reasons, full-row samples.
- Bottom console: real-time logs and scan errors.

## Visual Rules

- Do not use blue-purple gradients, floating decorative shapes, hero sections, oversized cards, emoji icons, or marketing copy.
- Keep panel radius at 6px or less.
- Use restrained borders and very light shadowing only when necessary.
- Prefer compact tables and data rows over large cards.
- Use low-contrast cold gray backgrounds.
- Risk colors are fixed:
  - High: red
  - Medium: amber
  - Low: teal green
- Use monospace text for logs, SQL-like data, field names, table paths, and sample values.

## Copy Rules

- Use concrete audit vocabulary: connection information, sensitive fields, existing row count, full-row samples, scan errors, export report.
- Avoid generic words such as Dashboard, Overview, Insights, Analytics, Hero, or Growth.
- Every screen state must explain what the scanner is doing or what evidence is available.

## AI-Generic Avoidance

- Do not ask an AI agent to "make it modern" or "make it clean" without these rules.
- Keep design decisions explicit before implementation: density, type scale, risk color, layout, and controls.
- Use mock data shaped like real database scan output so the interface is designed around actual work, not placeholder cards.
