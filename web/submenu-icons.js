(() => {
  "use strict";
  const icons={
    'email/add-domain':'add-mail-domain','email/create-mailbox':'create-mailbox',
    'email/forward':'create-alias','email/create-alias':'create-alias','email/webmail':'webmail','email/domains':'mail-domains',
    'email/mailboxes':'mailboxes','email/aliases':'aliases','email/delivery':'webmail',
    'email/smtp-port-check':'smtp-port-check','email/configure-webmail':'webmail',
    'settings/password':'admin-password','settings/email':'admin-email','settings/alerts':'alerts',
    'settings/appearance':'appearance','settings/timezone':'timezone','settings/hostname':'hostname','settings/dns':'dns',
    'settings/developer':'developer','settings/update':'update','settings/reboot':'reboot'
  };
  document.querySelectorAll('#sidebar .settings-submenu a').forEach(link=>{
    if(link.querySelector('img,svg'))return;
    const icon=icons[link.getAttribute('href')?.replace(/^#/,'')];if(!icon)return;
    const label=document.createElement('span');label.textContent=link.textContent;
    const image=document.createElement('img');image.src=`assets/sidebar/${icon}.svg`;image.alt='';image.width=20;image.height=20;image.className='submenu-feature-icon';
    link.replaceChildren(image,label);
  });
})();
