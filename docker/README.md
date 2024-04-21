## docker-compose.yml

Change your lnaddrd's volume and lnd's volume according your config:

* /your/volume/lnaddrd 
* /your/volume/lnd

## lnaddrd.yaml

* change url_authority according your domain
* change lightning_address_usernames
* change short_description
* copy your icon.png to /your/volume/lnaddrd
* set your lnd host

## build

build your docker container with :

`docker compose build`

launch with

`docker compose up -d`
