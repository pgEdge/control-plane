# `gather-e2e-db-info.sh`

This is a script to fetch and archive logs and other information for a database
from a running E2E fixture.

- [`gather-e2e-db-info.sh`](#gather-e2e-db-infosh)
  - [Background](#background)
  - [Prerequisites](#prerequisites)
  - [Usage](#usage)
    - [Example](#example)

## Background

The information that we need to troubleshoot database errors can be spread
across multiple files, containers, and API endpoints. This script automates the
process of gathering that information into a single archive that can be shared
with the engineering team or attached to a ticket.

> [!CAUTION]
> This script tries to avoid gathering sensitive information, but this is not
> guaranteed. Please review the gathered information and exercise caution when
> publishing or sharing the output of this script.

## Prerequisites

This script `jq`, `yq`, and utilities from `coreutils`.

```sh
brew install jq yq coreutils
```

## Usage

```sh
./hack/gather-e2e-db-info.sh <path to test_config.yaml> <database ID>
```

This script takes two inputs:

1. The path to the `test_config.yaml` file for the target E2E fixture
2. The ID of the target database

### Example

```sh
./hack/gather-e2e-db-info.sh ./e2e/fixtures/outputs/ec2.test_config.yaml storefront
```
