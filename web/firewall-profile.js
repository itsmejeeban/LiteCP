(() => {
  const heading = document.querySelector('[data-page-view="firewall"] .page-heading');
  const button = document.createElement('button');
  button.className = 'button button-primary';
  button.textContent = 'Apply LiteCP Firewall';
  button.type = 'button';
  heading.insertBefore(button, document.getElementById('firewallBackend'));
  button.addEventListener('click', async () => {
    if (!await window.LiteCPDialogs.confirm('Allow the ports required by LiteCP and installed mail, webmail and FTP services? Existing active rules will be saved permanently before reloading the firewall. Required-port allow rules take precedence over custom blocks. External cloud firewalls must be configured separately.', {title:'Apply LiteCP Firewall', confirmText:'Apply firewall'})) return;
    button.disabled = true;
    try {
      const result = await window.LiteCP.api('/api/firewall/apply-litecp', {method:'POST', body:{}});
      window.LiteCP.showToast('LiteCP firewall applied', `Inbound: ${result.inbound.join(', ')}. Outbound: ${result.outbound.join(', ')}.`);
    } catch (error) { window.LiteCP.showToast('Firewall could not be fully applied', error.message); }
    finally { button.disabled = false; }
  });
})();
