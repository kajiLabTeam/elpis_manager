#!/bin/bash

# Start the Go server in the background
make run &
SERVER_PID=$!

# Wait for the server to start
sleep 3

# Define test files
wifi_file="wifi_data.csv"
ble_file_with_mac="ble_data_with_mac.csv"
ble_file_without_mac="ble_data_without_mac.csv"

# Create test WiFi CSV file
cat <<EOL > $wifi_file
UNIX,BSSID,RSSI
1622551234,00:14:22:01:23:45,-45
1622551267,00:25:96:FF:FE:0C,-55
EOL

# Create test BLE CSV file with target MAC address
cat <<EOL > $ble_file_with_mac
UNIXTIME,MACADDRESS,RSSI,ServiceData,ServiceUUIDs
1622551234,2E-3C-A8-03-7C-0A,-65,0201060303AAFE1716AAFE10F31200,0000AAFE-0000-1000-8000-00805F9B34FB
1622551267,B2:C3:D4:E5:F6:A1,-70,0201060303AAFE0F16AAFE10F31234,0000FEAA-0000-1000-8000-00805F9B34FB
EOL

# Create test BLE CSV file without target MAC address
cat <<EOL > $ble_file_without_mac
UNIXTIME,MACADDRESS,RSSI,ServiceData,ServiceUUIDs
1622551234,A1:B2:C3:D4:E5:F6,-65,0201060303AAFE1716AAFE10F31200,0000AAFE-0000-1000-8000-00805F9B34FB
1622551267,B2:C3:D4:E5:F6:A1,-70,0201060303AAFE0F16AAFE10F31234,0000FEAA-0000-1000-8000-00805F9B34FB
EOL

# Test case: BLE data with target MAC address
echo "Testing BLE data with target MAC address..."
response=$(curl -s -F "wifi_data=@$wifi_file" -F "ble_data=@$ble_file_with_mac" http://localhost:8010/api/signals/submit)
echo "Response: $response"

# Test case: BLE data without target MAC address
echo "Testing BLE data without target MAC address..."
response=$(curl -s -F "wifi_data=@$wifi_file" -F "ble_data=@$ble_file_without_mac" http://localhost:8010/api/signals/submit)
echo "Response: $response"

# Stop the Go server
kill $SERVER_PID

# Clean up test files
rm $wifi_file $ble_file_with_mac $ble_file_without_mac
