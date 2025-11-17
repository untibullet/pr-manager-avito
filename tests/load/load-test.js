import http from 'k6/http';
import { check, sleep } from 'k6';

// --- Конфигурация теста ---
export const options = {
  vus: 10,
  duration: '30s',
};

// --- Основной сценарий теста ---
export default function () {
  const baseUrl = 'http://localhost:8081';
  const headers = { 'Content-Type': 'application/json' };

  // --- Генерация уникальных данных внутри цикла ---
  const teamName = `load-test-team-${__VU}-${__ITER}`;
  const prId = `pr-${__VU}-${__ITER}`;
  const userId = `u-${__VU}-1`;

  // Создание команды
  const teamPayload = JSON.stringify({
    team_name: teamName,
    members: [
      { user_id: `${userId}`, username: 'Alice', is_active: true },
      { user_id: `u-${__VU}-2`, username: 'Bob', is_active: true },
    ],
  });
  
  const teamRes = http.post(`${baseUrl}/team/add`, teamPayload, { headers });
  check(teamRes, {
    'team created successfully': (r) => r.status === 201,
  });

  sleep(1);

  // Создание Pull Request
  const prPayload = JSON.stringify({
    pull_request_id: prId,
    pull_request_name: 'Feature: New awesome thing',
    author_id: userId,
  });

  const prRes = http.post(`${baseUrl}/pullRequest/create`, prPayload, { headers });
  check(prRes, {
    'pr created successfully': (r) => r.status === 201,
  });
  
  sleep(1);

  // Запрос общей статистики
  const statsRes = http.get(`${baseUrl}/stats`);
  check(statsRes, {
    'stats fetched successfully': (r) => r.status === 200,
  });
}
