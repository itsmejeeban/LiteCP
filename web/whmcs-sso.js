(() => {
    'use strict';
    const ticket = location.hash.slice(1);
    // Remove the bearer ticket before any request or navigation.
    history.replaceState(null, '', location.pathname);
    const status = document.getElementById('sso-status');
    if (!/^[A-Za-z0-9_-]{40,128}$/.test(ticket)) {
        status.textContent = 'Missing or invalid login ticket. Open a new login from WHMCS.';
        return;
    }
    fetch('/api/whmcs/sso', {
        method: 'POST',
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({ticket})
    }).then(async response => {
        if (!response.ok) {
            throw new Error('Login expired or was rejected. Open a new login from WHMCS.');
        }
        const result = await response.json();
        if (result.ok !== true) {
            throw new Error('LiteCP could not complete login. Retry from WHMCS.');
        }
        location.replace('/');
    }).catch(() => {
        status.textContent = 'Login failed or expired. Open a new login from WHMCS.';
    });
})();
