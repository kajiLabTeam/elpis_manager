#!/bin/bash

# Define variables
GO_APP_PORT="8010"  # Adjust if your server runs on a different port
BLE_CSV_FILE="ble_data.csv"
WIFI_CSV_FILE="wifi_data.csv"

# Create test BLE CSV data
cat > $BLE_CSV_FILE <<EOL
timestamp,uuid,rssi
$(date +%s),8ebc2114-4abd-ba0d-b7c6-ff0a00200050,-60
$(date +%s),00000000-1111-2222-3333-444444444444,-80
EOL

# Create test WiFi CSV data
cat > $WIFI_CSV_FILE <<EOL
timestamp,bssid,rssi
$(date +%s),66:77:88:99:AA:BB,-60
$(date +%s),66:77:88:99:AA:BC,-80
EOL

echo "Sending test data to /api/signals/submit..."

# Send data to /api/signals/submit
RESPONSE=$(curl -s -F "ble_data=@$BLE_CSV_FILE" -F "wifi_data=@$WIFI_CSV_FILE" http://localhost:$GO_APP_PORT/api/signals/submit)
echo "Response from /api/signals/submit: $RESPONSE"

echo "Sending test data to /api/signals/server..."

# Send data to /api/signals/server
RESPONSE=$(curl -s -F "ble_data=@$BLE_CSV_FILE" -F "wifi_data=@$WIFI_CSV_FILE" http://localhost:$GO_APP_PORT/api/signals/server)
echo "Response from /api/signals/server: $RESPONSE"

# Clean up
rm -f $BLE_CSV_FILE $WIFI_CSV_FILE

echo "Test completed."
