// src/App.tsx
import 'sanitize.css'; // sanitize.css をインポート
import React, { useEffect, useState } from "react";
import {
  Container,
  Typography,
  CircularProgress,
  Alert,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  Box,
  Grid,
  List,
  ListItem,
  ListItemText,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
} from "@mui/material";

// 型定義（必要に応じて追加・修正してください）
interface PresenceSession {
  session_id: number;
  user_id: number;
  room_id: number;
  start_time: string;
  end_time: string | null;
  last_seen: string;
}

interface UserPresenceDay {
  date: string;
  sessions: PresenceSession[];
}

interface PresenceHistoryResponse {
  history: UserPresenceDay[];
}

interface CurrentOccupant {
  user_id: string;
  last_seen: string;
}

interface RoomOccupants {
  room_id: number;
  room_name: string;
  occupants: CurrentOccupant[];
}

interface CurrentOccupantsResponse {
  rooms: RoomOccupants[];
}

const App: React.FC = () => {
  const [presenceHistory, setPresenceHistory] = useState<
    UserPresenceDay[] | null
  >(null);
  const [currentOccupants, setCurrentOccupants] = useState<
    RoomOccupants[] | null
  >(null);
  const [loadingHistory, setLoadingHistory] = useState<boolean>(true);
  const [loadingOccupants, setLoadingOccupants] = useState<boolean>(true);
  const [errorHistory, setErrorHistory] = useState<string | null>(null);
  const [errorOccupants, setErrorOccupants] = useState<string | null>(null);

  // サーバーURLの選択肢
  const serverOptions = [
    { label: "本番環境", value: "https://elpis-m1.kajilab.dev" },
    { label: "開発環境", value: "http://localhost:8010" },
  ];

  // サーバーURLを状態として管理
  const [serverUrl, setServerUrl] = useState<string>(
    "https://elpis-m1.kajilab.dev"
  );
  const USER_ID = 1;

  // サーバーURLが変更されたときにデータを再フェッチ
  useEffect(() => {
    // データを取得する関数
    const fetchData = async () => {
      setLoadingHistory(true);
      setErrorHistory(null);
      setPresenceHistory(null);

      setLoadingOccupants(true);
      setErrorOccupants(null);
      setCurrentOccupants(null);

      // 在室履歴の取得
      const fetchPresenceHistory = async () => {
        try {
          const response = await fetch(
            `${serverUrl}/api/presence_history?user_id=${USER_ID}`,
            {
              headers: {
                Accept: "application/json",
              },
            }
          );
          if (!response.ok) {
            throw new Error(
              `Error: ${response.status} ${response.statusText}`
            );
          }
          const data: PresenceHistoryResponse = await response.json();
          setPresenceHistory(data.history);
        } catch (error: any) {
          setErrorHistory(error.message);
        } finally {
          setLoadingHistory(false);
        }
      };

      // 現在の在室者情報の取得
      const fetchCurrentOccupants = async () => {
        try {
          const response = await fetch(`${serverUrl}/api/current_occupants`, {
            headers: {
              Accept: "application/json",
            },
          });
          if (!response.ok) {
            throw new Error(
              `Error: ${response.status} ${response.statusText}`
            );
          }
          const data: CurrentOccupantsResponse = await response.json();
          setCurrentOccupants(data.rooms);
        } catch (error: any) {
          setErrorOccupants(error.message);
        } finally {
          setLoadingOccupants(false);
        }
      };

      // 並行してデータを取得
      await Promise.all([fetchPresenceHistory(), fetchCurrentOccupants()]);
    };

    fetchData();
  }, [serverUrl]); // serverUrl が変更されるたびに再実行

  // ドロップダウンの変更ハンドラー
  const handleServerChange = (
    event: React.ChangeEvent<{ value: unknown }>
  ) => {
    setServerUrl(event.target.value as string);
  };

  return (
    <Container maxWidth="lg" sx={{ padding: "20px" }}>
      <Box mb={4} display="flex" alignItems="center" justifyContent="space-between">
        <Typography variant="h4" gutterBottom>
          在室履歴と現在の在室者情報
        </Typography>
        {/* サーバー選択ドロップダウン */}
        <FormControl variant="outlined" size="small" sx={{ minWidth: 200 }}>
          <InputLabel id="server-select-label">サーバー選択</InputLabel>
          <Select
            labelId="server-select-label"
            id="server-select"
            value={serverUrl}
            onChange={handleServerChange}
            label="サーバー選択"
          >
            {serverOptions.map((option) => (
              <MenuItem key={option.value} value={option.value}>
                {option.label}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      </Box>

      {/* 在室履歴セクション */}
      <Box mb={6}>
        <Typography variant="h5" gutterBottom>
          在室履歴
        </Typography>
        {loadingHistory ? (
          <Box display="flex" alignItems="center">
            <CircularProgress size={24} />
            <Typography variant="body1" ml={2}>
              在室履歴を取得中...
            </Typography>
          </Box>
        ) : errorHistory ? (
          <Alert severity="error">エラー: {errorHistory}</Alert>
        ) : presenceHistory && presenceHistory.length > 0 ? (
          presenceHistory.map((day) => (
            <Box key={day.date} mb={4}>
              <Typography variant="h6" gutterBottom>
                {day.date}
              </Typography>
              <TableContainer component={Paper} elevation={3}>
                <Table>
                  <TableHead>
                    <TableRow>
                      <TableCell>セッションID</TableCell>
                      <TableCell>ユーザーID</TableCell>
                      <TableCell>部屋ID</TableCell>
                      <TableCell>開始時間</TableCell>
                      <TableCell>終了時間</TableCell>
                      <TableCell>最終確認時間</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {day.sessions.map((session) => (
                      <TableRow key={session.session_id}>
                        <TableCell>{session.session_id}</TableCell>
                        <TableCell>{session.user_id}</TableCell>
                        <TableCell>{session.room_id}</TableCell>
                        <TableCell>
                          {new Date(session.start_time).toLocaleString()}
                        </TableCell>
                        <TableCell>
                          {session.end_time
                            ? new Date(session.end_time).toLocaleString()
                            : "N/A"}
                        </TableCell>
                        <TableCell>
                          {new Date(session.last_seen).toLocaleString()}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            </Box>
          ))
        ) : (
          <Typography variant="body1">在室履歴が見つかりません。</Typography>
        )}
      </Box>

      {/* 現在の在室者情報セクション */}
      <Box>
        <Typography variant="h5" gutterBottom>
          現在の在室者情報
        </Typography>
        {loadingOccupants ? (
          <Box display="flex" alignItems="center">
            <CircularProgress size={24} />
            <Typography variant="body1" ml={2}>
              現在の在室者情報を取得中...
            </Typography>
          </Box>
        ) : errorOccupants ? (
          <Alert severity="error">エラー: {errorOccupants}</Alert>
        ) : currentOccupants && currentOccupants.length > 0 ? (
          <Grid container spacing={4}>
            {currentOccupants.map((room) => (
              <Grid item xs={12} md={6} key={room.room_id}>
                <Paper elevation={3} sx={{ padding: "16px" }}>
                  <Typography variant="h6" gutterBottom>
                    {room.room_name} (ID: {room.room_id})
                  </Typography>
                  {room.occupants.length > 0 ? (
                    <List>
                      {room.occupants.map((occupant) => (
                        <ListItem key={occupant.user_id} disablePadding>
                          <ListItemText
                            primary={`ユーザーID: ${occupant.user_id}`}
                            secondary={`最終確認時間: ${new Date(
                              occupant.last_seen
                            ).toLocaleString()}`}
                          />
                        </ListItem>
                      ))}
                    </List>
                  ) : (
                    <Typography variant="body2">
                      現在在室者はいません。
                    </Typography>
                  )}
                </Paper>
              </Grid>
            ))}
          </Grid>
        ) : (
          <Typography variant="body1">
            現在の在室者情報が見つかりません。
          </Typography>
        )}
      </Box>
    </Container>
  );
};

export default App;
