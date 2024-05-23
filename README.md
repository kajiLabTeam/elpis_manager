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
curl -X POST -F "file=@test.csv" http://localhost:8080/upload
```
