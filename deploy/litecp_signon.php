<?php
// One-time, administrator-authorized mailbox sign-in. No credentials in URLs.
class litecp_signon extends rcube_plugin
{
    public function init()
    {
        $rc = rcmail::get_instance();
        if (!empty($_SESSION['litecp_master_login'])) {
            $this->configure($rc, $rc->get_user_password());
        }
        $this->add_hook('startup', [$this, 'startup']);
    }
    private function configure($rc, $password)
    {
        $rc->config->set('imap_auth_type', 'PLAIN');
        $rc->config->set('imap_auth_cid', 'litecp-webmail-sso');
        $rc->config->set('imap_auth_pw', $password);
        $rc->config->set('smtp_auth_type', 'PLAIN');
        $rc->config->set('smtp_auth_cid', 'litecp-webmail-sso');
        $rc->config->set('smtp_auth_pw', $password);
        $rc->get_storage()->set_options(['auth_type' => 'PLAIN', 'auth_cid' => 'litecp-webmail-sso', 'auth_pw' => $password]);
    }
    public function startup($args)
    {
        if ($_SERVER['REQUEST_METHOD'] !== 'POST' || !isset($_POST['litecp_ticket'])) {
            return $args;
        }
        header('Cache-Control: no-store');
        header('Referrer-Policy: no-referrer');
        if (!preg_match('/^[A-Za-z0-9_-]{40,100}$/', $_POST['litecp_ticket'])) {
            http_response_code(403); exit('Open this mailbox again from LiteCP.');
        }
        $context = stream_context_create(['http' => ['method' => 'POST', 'header' => "Content-Type: application/json\r\n", 'content' => json_encode(['ticket' => $_POST['litecp_ticket']]), 'timeout' => 10, 'ignore_errors' => true]]);
        $result = @file_get_contents('http://127.0.0.1:9090/internal/webmail-ticket', false, $context);
        $credentials = json_decode($result ?: '', true);
        if (!is_array($credentials) || empty($credentials['address']) || empty($credentials['password'])) {
            http_response_code(403); exit('Mailbox login expired. Open this mailbox again from LiteCP.');
        }
        $rc = rcmail::get_instance();
        // Replace an existing mailbox session before opening the selected one.
        $rc->logout_actions();
        $rc->kill_session();
        $this->configure($rc, $credentials['password']);
        if (!$rc->login($credentials['address'], $credentials['password'])) {
            http_response_code(502); exit('Mailbox sign-in failed. Please contact the administrator.');
        }
        $_SESSION['litecp_master_login'] = true;
        $rc->session->regenerate_id(false);
        $rc->session->set_auth_cookie();
        // Commit a same-origin document before navigating, so Strict cookies
        // work even when the panel is reached by IP and webmail by hostname.
        header('Content-Type: text/html; charset=UTF-8');
        $nonce = base64_encode(random_bytes(18));
        header("Content-Security-Policy: default-src 'none'; script-src 'nonce-" . $nonce . "'; base-uri 'none'; frame-ancestors 'none'");
        // JavaScript navigation runs after this webmail-origin document commits.
        // The meta refresh is a fallback; neither path needs another user click.
        echo '<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=/?_task=mail"><title>Mailbox</title></head><body><script nonce="' . $nonce . '">window.location.replace("/?_task=mail");</script></body></html>';
        exit;
    }
}
