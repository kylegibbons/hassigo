


#docker run --rm --privileged -v ~/.docker:/root/.docker -v addon:/data:ro homeassistant/amd64-builder --all -t /data

docker run --rm --privileged -v ~/.docker:/root/.docker homeassistant/amd64-builder --all -t addon -r https://github.com/kylegibbons/hassigo -b master
