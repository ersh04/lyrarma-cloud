# Remote users database

This directory contains everything needed to run the dedicated users PostgreSQL database on another server

## Requirements

- Docker with the Compose plugin
- OpenSSL
- A private network or VPN between the application and database servers
- A firewall rule allowing TCP port 5432 only from the application server

## Start

Copy this entire directory to the database server and run

```sh
chmod 755 run.sh
./run.sh
```

On the first run, the script creates `.env` with a random URL-safe database password and starts PostgreSQL

The generated `.env` uses `0.0.0.0:5432` so the database can accept remote connections. Restrict this port with the server firewall before starting the database

Example UFW rule where `10.20.0.20` is the private application server address

```sh
sudo ufw allow from 10.20.0.20 to any port 5432 proto tcp
```

Read the generated credentials

```sh
chmod 600 .env
sed -n "1,3p" .env
```

Configure the application server with the generated password and the private address of this database server

```dotenv
USERS_DATABASE_URL="postgres://lyrarma:GENERATED_PASSWORD@DATABASE_PRIVATE_IP:5432/lyrarma-services-users?sslmode=disable"
```

Use this configuration only across a trusted private network or VPN. Configure PostgreSQL TLS and use `sslmode=verify-full` when traffic crosses an untrusted network

## Stop

```sh
docker compose -f compose.yaml down
```

The named Docker volume is preserved when the container stops
