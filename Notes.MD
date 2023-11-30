## steps followed

cloudflare/cf-terraforming
dbt-cloud/dbtcloud-terraforming

cf-terraforming
dbtcloud-terraforming

Renaming folders

## how to use

Set env vars

```sh
export DBT_CLOUD_API_TOKEN=<token>
export DBT_CLOUD_ACCOUNT_ID=123
export DBT_CLOUD_HOSTNAME="emea.dbt.com"
```

### to use directly

```sh
go run cmd/dbtcloud-terraforming/main.go generate --resource-type dbtcloud_environment
go run cmd/dbtcloud-terraforming/main.go import --resource-type dbtcloud_environment
```

### to build and use

```sh
go build -o dbtcloud-terraforming cmd/dbtcloud-terraforming/main.go
./dbtcloud-terraforming generate --resource-type dbtcloud_job 
```
