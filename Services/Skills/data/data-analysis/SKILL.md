---
name: data-analysis
description: Clean, analyze, validate, and explain structured data.
---

# Data analysis workflow

Use for spreadsheet analysis, metric design, segmentation, experiments, trends, and anomaly investigation.

## Process

1. Define the question, population, grain, period, metric formula, and expected output.
2. Profile the source before transforming it: columns, types, units, row count, missingness, duplicates, and obvious outliers.
3. Keep source data unchanged and document each filter, join, conversion, and exclusion.
4. Validate intermediate totals and reconcile the final population to the source.
5. Check whether the result changes under reasonable alternative definitions.
6. Report the finding with sample size, units, period, limitations, and enough method detail to reproduce it.

## Rules

- Never fill missing values or remove outliers silently.
- Do not describe correlation as causation.
- Distinguish statistical significance from practical importance.
- Preserve leading zeros, identifiers, time zones, and currency units.
- Prefer a simple table or chart that answers the question over a decorative dashboard.
