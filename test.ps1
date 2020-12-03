
#docker run --rm --privileged -v ~/.docker:/root/.docker -v addon:/data:ro homeassistant/amd64-builder --all -t /data

#docker run --rm --privileged -v ~/.docker:/root/.docker tes --all -t addon -r https://github.com/kylegibbons/hassigo -b master --generic 1

docker run -it --rm --privileged -v "$(pwd)/addon:/addon" homeassistant/amd64-builder --amd64 -t /addon --test


#docker run -it --privileged -v ./addon/:/addon ubuntu bash