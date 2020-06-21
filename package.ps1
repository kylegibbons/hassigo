remove-item -Recurse -Path ./addon/bin | Out-Null

$env:CGO_ENABLED = 0 
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -a -o ./addon/bin/hassigo

copy-item .\raw.html ./addon/bin | Out-Null

docker build -t kylegibbons/hassigo --build-arg BUILD_FROM=hassioaddons/base-amd64:7.2.0 ./addon 