# Dev Environment

The `docker` environment configuration is setup in the codebase such that it can be used to setup the development environment in quickest manner. Its configuration currently is tested on Ubuntu.

On Ubuntu/Debian systems the dependencies for development environment are -
 * **Docker** version 1.9.1+
 * **docker-compose** version 1.8.0+

Since we are using one docker container for both API and UI build and development, hence the docker image size is larger than usual, so the first download size would be approx. 1GB+. 

Once Docker and docker-compose is setup just execute the following command -
```
docker-compose run --rm fs-image-manager bash
make setup-docker-dev
```
What the `make` command here does is, moves the built web project (minified JS and CSS) to another location and symlinks the dev folders directly into the *dist* directory.

If you notice the docker-compose file all the meaningful source code is already "mounted" into the container, so if you change your source code they should reflect in there; but for **aurelia** to build the frontend code, we will need to run its builder. We can do that -
```
docker exec -it $(docker ps --filter "ancestor=imyousuf/fs-image-manager:latest" -q) bash
cd web/img-mngr/
au build --watch
```
This should build the frontend source code every time it changes.

Now the last step would be to run the backend; this does not auto load code if it changes so this we will need to run everytime the code chages
```
go install
(cd dist/ && fs-image-manager )
```
Please use `CTRL+C` to exit the backend process. If you want to see the backend log in the console, just edit the log appender to not send logs to file by renaming `[log]` -> `[alog]` or anything else or removing the section altogether.
```
apk add nano
nano dist/image-manager.cfg
```
These commands (except for config edit) are summarized in [this](https://gist.github.com/imyousuf/0e515fc9bcd5ff03f7967a1ea9f11128) script. When using this script you may mount it into the container using `docker-compose.override.yml` and you use `custom-scripts` directory to keep the script in the codebase. Both of these paths are ignored in git and docker.

Please note that docker-compose also mounts the **Pictures** directory in *User Home* to serve images in the app; please use docker-compose override mechanism to point to the right test image storage location locally.
