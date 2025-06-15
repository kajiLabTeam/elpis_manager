curl -X POST http://localhost:8012/api/register \
  -H 'Content-Type: application/json' \
  -d '{"management_server_url":"https://orgA.example.com","proxy_server_url":"https://proxA.example.com","mapping":[{"floor":"1","room_id":"101","room_name":"第一会議室"},{"floor":"2","room_id":"201","room_name":"第二会議室"}]}'
