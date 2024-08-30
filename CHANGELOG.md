# Changelog

## 0.8.0 (2024-08-30)

- Add `dbtcloud_global_connection` and the ability to link it to environments
- Remove `dbtcloud_project_connection` now that connections are set at the environment level

## 0.7.0

- Add import for notifications on warning for jobs

## 0.6.0 (2024-06-28)

- Add support for on_merge triggers for jobs and update testing
- Limit the notifications to generate/import for external ones
- Add support for importing/generating service tokens
- Add support for importing/generating databricks credentials

## 0.5.0 (2024-04-11)

- Allow importing jobs with job completion triggers

## 0.4.1 (2024-04-10)

- Fix linking repositories and credentials for Snowflake and BigQuery

## 0.4.0 (2024-01-23)

- Add support for `dbtcloud_webhook` and `dbtcloud_notification`

## 0.3.0

- Add support for `dbtcloud_user_groups`

## 0.2.0

- Add CI/CD to release to homebrew

## 0.1.0 (2023-11-30)

- Initial creation of the repo, a modified copy of `cf-terraforming`
