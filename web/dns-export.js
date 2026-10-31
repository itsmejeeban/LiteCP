/* Portable BIND zone export for the generated mail DNS records. */
(function(root){
  'use strict';
  function fqdn(value){
    const name=String(value).replace(/\.$/,'');
    if(name.length>253||!name.split('.').every(label=>/^(?:[a-z0-9_]|[a-z0-9_][a-z0-9_-]{0,61}[a-z0-9_])$/i.test(label)))throw Error('Invalid DNS record name.');
    return name+'.';
  }
  function txt(value){
    const bytes=new TextEncoder().encode(String(value)),parts=[];
    for(let i=0;i<bytes.length||i===0;i+=255){
      let part='';
      for(const byte of bytes.slice(i,i+255))part+=byte===34||byte===92?'\\'+String.fromCharCode(byte):byte<32||byte>126?'\\'+String(byte).padStart(3,'0'):String.fromCharCode(byte);
      parts.push('"'+part+'"');
    }
    return parts.join(' ');
  }
  function zone(domain,records){
    const lines=['; LiteCP mail DNS records for '+fqdn(domain),'; Import into your DNS provider. Review existing SPF records before importing.','; Mail host A/AAAA records must be DNS only (not proxied).','$ORIGIN '+fqdn(domain),'$TTL 3600'];
    for(const record of records){
      const type=String(record.type).toUpperCase(),name=fqdn(record.name);let value=String(record.value);
      if(type==='TXT')value=txt(value);
      else if(type==='MX'){
        const match=value.match(/^(\d{1,5})\s+(\S+)$/);
        if(!match||Number(match[1])>65535)throw Error('Invalid MX record.');
        value=Number(match[1])+' '+fqdn(match[2]);
      }else if(type==='A'){
        if(!/^\d{1,3}(\.\d{1,3}){3}$/.test(value)||value.split('.').some(x=>Number(x)>255))throw Error('Invalid IPv4 record.');
      }else if(type==='AAAA'){
        if(!/^[0-9a-f:]+$/i.test(value)||!value.includes(':'))throw Error('Invalid IPv6 record.');
      }else throw Error('Unsupported DNS record type: '+type);
      lines.push(name+' 3600 IN '+type+' '+value);
    }
    return lines.join('\n')+'\n';
  }
  root.LiteCPDNSExport={zone};
  if(typeof module==='object'&&module.exports)module.exports={zone};
})(typeof window==='object'?window:globalThis);
