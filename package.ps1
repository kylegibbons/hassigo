remove-item -Recurse -Path ./addon/bin | Out-Null

$env:CGO_ENABLED = 0 
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -a -o ./addon/bin/hassigo ./cmd/hassigo

copy-item ./cmd/hassigo/raw.html ./addon/bin | Out-Null

#docker build -t kylegibbons/hassigo --build-arg BUILD_FROM=alpine ./addon 