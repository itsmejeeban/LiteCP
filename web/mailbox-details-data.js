(function(root){
  'use strict';
  function rows(address,hostname){return [
    ['Email address',address],['Username',address],['Password','Your mailbox password (not stored or displayed here)'],
    ['Incoming protocol','IMAP (recommended)'],['Incoming server',hostname],['IMAP port','993'],['IMAP encryption','SSL/TLS'],
    ['Outgoing server',hostname],['SMTP port','465'],['SMTP encryption','SSL/TLS'],
    ['Alternative SMTP port','587'],['Alternative SMTP encryption','STARTTLS'],['SMTP authentication','Required - same full email address and mailbox password'],
    ['POP3','Not enabled on this server. Use IMAP.'],['Secure POP3 standard port','995 (unavailable unless POP3 is separately enabled)']
  ];}
  root.LiteCPMailboxDetails={rows};if(typeof module==='object'&&module.exports)module.exports={rows};
})(typeof window==='object'?window:globalThis);
