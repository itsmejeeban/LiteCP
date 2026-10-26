<?php
// Redeem a short-lived, single-use ticket through the local LiteCP process.
header('Cache-Control: no-store');
header('Referrer-Policy: no-referrer');
if ($_SERVER['REQUEST_METHOD'] !== 'POST' || !preg_match('/^[A-Za-z0-9_-]{40,100}$/', $_POST['ticket'] ?? '')) {
    http_response_code(400); exit('Open a database from the LiteCP Databases page.');
}
$context = stream_context_create(['http' => ['method' => 'POST', 'header' => "Content-Type: application/json\r\n", 'content' => json_encode(['ticket' => $_POST['ticket']]), 'timeout' => 10, 'ignore_errors' => true]]);
$result = @file_get_contents('http://127.0.0.1:9090/internal/phpmyadmin-ticket', false, $context);
$credentials = json_decode($result ?: '', true);
if (!is_array($credentials) || !isset($credentials['username'], $credentials['password'], $credentials['database'])) {
    http_response_code(403); exit('Database access expired. Open the database again from LiteCP.');
}
session_name('LiteCPSignon');
session_set_cookie_params(['lifetime'=>0,'path'=>'/api/phpmyadmin/','secure'=>true,'httponly'=>true,'samesite'=>'Strict']);
session_start();
// No session destruction: retain files and replace only this session's data.
session_regenerate_id(false);
$_SESSION = ['PMA_single_signon_user'=>$credentials['username'], 'PMA_single_signon_password'=>$credentials['password'], 'PMA_single_signon_host'=>'localhost', 'PMA_single_signon_port'=>'', 'PMA_single_signon_cfgupdate'=>['only_db'=>[$credentials['database']]]];
session_write_close();
header('Location: /api/phpmyadmin/index.php?route=/database/structure&db='.rawurlencode($credentials['database']), true, 303);
