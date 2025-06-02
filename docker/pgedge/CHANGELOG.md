# pgEdge Image Changelog

## Unreleased

## [4.0.10-4] - 2025-05-08

### Changed

#### pg15 variant

- Upgraded to PostgreSQL 15.13-1

#### pg16 variant

- Upgraded to PostgreSQL 16.9-2

#### pg17 variant

- Upgraded to PostgreSQL 17.5-2

#### All variants

- Install Patroni from `pip` instead of with system package manager
  - The system package manager provides outdated Python dependencies that
    contain several Medium and High CVEs. Installing from `pip` gives us the
    latest compatible package versions and resolves the CVEs.

## [4.0.10-3] - 2025-03-20

### Changed

#### All variants

- Changed host user to `postgres`

## [4.0.10-2] - 2025-02-27

### Changed

#### All variants

- Upgraded Patroni to 4.0.5-1
- Pinned python-json-logger to 3.2.1

### Added

#### All variants

- pgBackRest 2.54.2-1

## [4.0.10-1] - 2025-02-26

### Added

#### pg15 variant

- PostgreSQL 15.12-1

#### pg16 variant

- PostgreSQL 16.8-1

#### pg17 variant

- PostgreSQL 17.4-1

#### All variants

- Spock 4.0.10-1
- Snowflake 2.2-1
- LOLOR 1.2-1
- PostGIS 3.5.2-1
- pgvector 0.8.0-1
- python3-pip 21.3.1-1
- Patroni 4.0.4-1
- python-json-logger >= 2.0.2
