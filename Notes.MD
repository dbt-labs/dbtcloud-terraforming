## steps followed

cloudflare/cf-terraforming
dbt-cloud/dbtcloud-terraforming

cf-terraforming
dbtcloud-terraforming

Renaming folders

## how to use

Set env vars

```sh
export DBT_CLOUD_TOKEN=<token>
export DBT_CLOUD_ACCOUNT_ID=123
export DBT_CLOUD_HOST_URL="http://emea.dbt.com/api"
```

### to use directly

```sh
go run cmd/dbtcloud-terraforming/main.go generate --resource-types dbtcloud_environment
go run cmd/dbtcloud-terraforming/main.go import --resource-types dbtcloud_environment
```

### to build and use

```sh
go build -o dbtcloud-terraforming cmd/dbtcloud-terraforming/main.go
./dbtcloud-terraforming generate --resource-types dbtcloud_job 
```
