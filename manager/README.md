# Indoor Positioning System

This project is a system for indoor location estimation. It uses BLE beacons and Wi-Fi signals to estimate the current location of users and provide accurate location information.

## Requirements

- Go 1.22 or higher
- Docker / Docker Compose

## Installation

1. Clone the repository.

```sh
git@github.com:kajiLabTeam/elpis_manager.git
cd elpis_manager
```

1. Install the necessary dependencies.

```sh
go mod download
```

## Usage

### Starting the Server

Run the following command to start the server.

```sh
make run
```

### Uploading CSV Files

To upload a CSV file, send a POST request to the `/upload` endpoint with the file as a form-data parameter.

```sh
curl -X POST http://localhost:8010/api/signals/submit \
  -F "wifi_data=@./csv/wifi_data.csv" \
  -F "ble_data=@./csv/ble_data.csv"
```

```sh
curl -X POST http://localhost:8010/api/signals/server \
  -F "wifi_data=@./csv/wifi_data.csv" \
  -F "ble_data=@./csv/ble_data.csv"
```

```pwsh
$boundary = [System.Guid]::NewGuid().ToString()

$wifiData = [System.IO.File]::ReadAllBytes("./csv/wifi_data.csv")
$wifiContent = [System.Text.Encoding]::UTF8.GetString($wifiData)


$bleData = [System.IO.File]::ReadAllBytes("./csv/ble_data.csv")
$bleContent = [System.Text.Encoding]::UTF8.GetString($bleData)

$bodyLines = @(
    "--$boundary",
    'Content-Disposition: form-data; name="wifi_data"; filename="wifi_data.csv"',
    'Content-Type: text/csv',
    '',
    $wifiContent,
    "--$boundary",
    'Content-Disposition: form-data; name="ble_data"; filename="ble_data.csv"',
    'Content-Type: text/csv',
    '',
    $bleContent,
    "--$boundary--"
)

$body = $bodyLines -join "`r`n"

$response = Invoke-RestMethod -Uri "http://localhost:8080/api/signals/submit" -Method Post -ContentType "multipart/form-data; boundary=$boundary" -Body $body

$response | ConvertTo-Json
```

```pwsh
$boundary = [System.Guid]::NewGuid().ToString()

$wifiData = [System.IO.File]::ReadAllBytes("./csv/wifi_data.csv")
$wifiContent = [System.Text.Encoding]::UTF8.GetString($wifiData)

$bleData = [System.IO.File]::ReadAllBytes("./csv/ble_data.csv")
$bleContent = [System.Text.Encoding]::UTF8.GetString($bleData)

$bodyLines = @(
    "--$boundary",
    'Content-Disposition: form-data; name="wifi_data"; filename="wifi_data.csv"',
    'Content-Type: text/csv',
    '',
    $wifiContent,
    "--$boundary",
    'Content-Disposition: form-data; name="ble_data"; filename="ble_data.csv"',
    'Content-Type: text/csv',
    '',
    $bleContent,
    "--$boundary--"
)

$body = $bodyLines -join "`r`n"

$response = Invoke-RestMethod -Uri "http://localhost:8080/api/signals/server" -Method Post -ContentType "multipart/form-data; boundary=$boundary" -Body $body


$response | ConvertTo-Json
```
