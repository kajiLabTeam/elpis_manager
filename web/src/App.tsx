import React, { useEffect, useState } from "react";

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

  const SERVER_URL = "https://elpis-m1.kajilab.dev";
  const USER_ID = 1;

  useEffect(() => {
    // 在室履歴の取得
    const fetchPresenceHistory = async () => {
      try {
        const response = await fetch(
          `${SERVER_URL}/api/presence_history?user_id=${USER_ID}`,
          {
            headers: {
              Accept: "application/json",
            },
          }
        );
        if (!response.ok) {
          throw new Error(`Error: ${response.status} ${response.statusText}`);
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
        const response = await fetch(`${SERVER_URL}/api/current_occupants`, {
          headers: {
            Accept: "application/json",
          },
        });
        if (!response.ok) {
          throw new Error(`Error: ${response.status} ${response.statusText}`);
        }
        const data: CurrentOccupantsResponse = await response.json();
        setCurrentOccupants(data.rooms);
      } catch (error: any) {
        setErrorOccupants(error.message);
      } finally {
        setLoadingOccupants(false);
      }
    };

    fetchPresenceHistory();
    fetchCurrentOccupants();
  }, []);

  return (
    <div style={{ padding: "20px", fontFamily: "Arial, sans-serif" }}>
      <h1>在室履歴と現在の在室者情報</h1>

      {/* 在室履歴セクション */}
      <section style={{ marginBottom: "40px" }}>
        <h2>在室履歴</h2>
        {loadingHistory ? (
          <p>在室履歴を取得中...</p>
        ) : errorHistory ? (
          <p style={{ color: "red" }}>エラー: {errorHistory}</p>
        ) : presenceHistory && presenceHistory.length > 0 ? (
          presenceHistory.map((day) => (
            <div key={day.date} style={{ marginBottom: "20px" }}>
              <h3>{day.date}</h3>
              <table
                border={1}
                cellPadding={10}
                style={{ borderCollapse: "collapse", width: "100%" }}
              >
                <thead>
                  <tr>
                    <th>セッションID</th>
                    <th>ユーザーID</th>
                    <th>部屋ID</th>
                    <th>開始時間</th>
                    <th>終了時間</th>
                    <th>最終確認時間</th>
                  </tr>
                </thead>
                <tbody>
                  {day.sessions.map((session) => (
                    <tr key={session.session_id}>
                      <td>{session.session_id}</td>
                      <td>{session.user_id}</td>
                      <td>{session.room_id}</td>
                      <td>{new Date(session.start_time).toLocaleString()}</td>
                      <td>
                        {session.end_time
                          ? new Date(session.end_time).toLocaleString()
                          : "N/A"}
                      </td>
                      <td>{new Date(session.last_seen).toLocaleString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ))
        ) : (
          <p>在室履歴が見つかりません。</p>
        )}
      </section>

      {/* 現在の在室者情報セクション */}
      <section>
        <h2>現在の在室者情報</h2>
        {loadingOccupants ? (
          <p>現在の在室者情報を取得中...</p>
        ) : errorOccupants ? (
          <p style={{ color: "red" }}>エラー: {errorOccupants}</p>
        ) : currentOccupants && currentOccupants.length > 0 ? (
          currentOccupants.map((room) => (
            <div key={room.room_id} style={{ marginBottom: "20px" }}>
              <h3>
                {room.room_name} (ID: {room.room_id})
              </h3>
              {room.occupants.length > 0 ? (
                <ul>
                  {room.occupants.map((occupant) => (
                    <li key={occupant.user_id}>
                      ユーザーID: {occupant.user_id}, 最終確認時間:{" "}
                      {new Date(occupant.last_seen).toLocaleString()}
                    </li>
                  ))}
                </ul>
              ) : (
                <p>現在在室者はいません。</p>
              )}
            </div>
          ))
        ) : (
          <p>現在の在室者情報が見つかりません。</p>
        )}
      </section>
    </div>
  );
};

export default App;
