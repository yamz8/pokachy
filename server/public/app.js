const $ = id => document.getElementById(id);
let config, state, widget, turnstileToken = "";
const userCode = new URLSearchParams(location.search).get("user_code");
const fragment = new URLSearchParams(location.hash.slice(1));
if (fragment.has("email")) sessionStorage.setItem("pokachy-hints", JSON.stringify(Object.fromEntries(fragment)));
history.replaceState(null, "", location.pathname + location.search);
let hints = {};
try { hints = JSON.parse(sessionStorage.getItem("pokachy-hints") || "{}"); } catch { /* Optional hints only. */ }
function message(text, bad=false) { $("notice").textContent=text; $("notice").className=bad?"error":""; }
async function api(path, body, method=body===undefined?"GET":"POST", headers={}) {
  const response=await fetch(path,{method,headers:{"Content-Type":"application/json",...headers},body:body===undefined?undefined:JSON.stringify(body)});
  const data=await response.json();
  if(!response.ok) throw new Error(data.error?.message||data.error||data.message||"Please try again.");
  return data;
}
function busy(form, callback) {
  form.addEventListener("submit",async e=>{e.preventDefault();const button=form.querySelector("button[type=submit]");button.disabled=true;message("");try{await callback();}catch(e){message(e.message,true);}finally{button.disabled=false;}});
}
async function refresh() {
  const session=await api("/api/auth/get-session");
  $("loading").hidden=true;
  $("auth").hidden=!!session;$("signout").hidden=!session;
  for(const id of ["profile-form","approve","connected"]) $(id).hidden=true;
  if(!session) return;
  state=await api("/api/state");
  if(!state.me.handle){$("profile-form").hidden=false;$("handle").value=hints.handle||"";$("card-title").textContent="What should friends call you?";return;}
  $("identity").textContent=`Hello, @${state.me.handle}.`;
  if(userCode){
    const device=await api(`/api/auth/device?user_code=${encodeURIComponent(userCode)}`);
    if(device.status!=="pending"||device.client_id!=="pokachy-cli") throw new Error("This device request is unavailable. Start pokachy init again.");
    $("approve").hidden=false;$("device-code").textContent=userCode;$("card-title").textContent="Your desktop is ready.";
  }
  else {$("connected").hidden=false;$("card-title").textContent="Make someone’s day.";$("link-github").hidden=!config.github;}
}
busy($("email-form"),async()=>{
  if(!config.local&&!turnstileToken) throw new Error("Complete the verification first.");
  try{await api("/api/auth/email-otp/send-verification-otp",{email:$("email").value,type:"sign-in"},"POST",{"X-Turnstile-Token":turnstileToken});
    $("email-form").hidden=true;$("code-form").hidden=false;$("local-code").hidden=!config.local;$("code").focus();message("Check your inbox for a six-digit code.");
  }finally{if(widget!==undefined){window.turnstile.reset(widget);turnstileToken="";}}
});
busy($("code-form"),async()=>{await api("/api/auth/sign-in/email-otp",{email:$("email").value,otp:$("code").value,name:hints.name||$("email").value.split("@")[0]});$("local-code").hidden=true;message("");await refresh();});
busy($("profile-form"),async()=>{await api("/api/profile",{handle:$("handle").value},"PUT");await refresh();});
$("change-email").onclick=()=>{$("email-form").hidden=false;$("code-form").hidden=true;$("local-code").hidden=true;message("");};
$("local-code").onclick=async()=>{try{const d=await api(`/api/dev/mail?email=${encodeURIComponent($("email").value)}`);$("code").value=d.otp||"";}catch(e){message(e.message,true);}};
async function github(link=false){try{const result=await api(link?"/api/auth/link-social":"/api/auth/sign-in/social",{provider:"github",callbackURL:location.origin+location.pathname+location.search});if(result.url) location.assign(result.url);}catch(e){message(e.message,true);}}
$("github").onclick=()=>github();$("link-github").onclick=()=>github(true);
$("signout").onclick=async()=>{try{await api("/api/auth/sign-out",{});location.assign("/account");}catch(e){message(e.message,true);}};
$("approve-button").onclick=async()=>{try{$("approve-button").disabled=true;await api("/api/auth/device/approve",{userCode});$("approve").hidden=true;$("connected").hidden=false;$("card-title").textContent="See you on your desktop.";message("Device connected. You can return to your terminal.");sessionStorage.removeItem("pokachy-hints");}catch(e){message(e.message,true);}finally{$("approve-button").disabled=false;}};
$("deny-button").onclick=async()=>{try{await api("/api/auth/device/deny",{userCode});$("approve").hidden=true;message("Device request denied.");}catch(e){message(e.message,true);}};
$("sessions-toggle").onclick=async()=>{try{const sessions=await api("/api/auth/list-sessions");$("sessions").replaceChildren();$("sessions").hidden=false;for(const s of sessions){const row=document.createElement("div");row.className="session";const label=document.createElement("span");label.textContent=s.userAgent||"Pokachy device";const revoke=document.createElement("button");revoke.className="text-button";revoke.textContent="Revoke";revoke.onclick=async()=>{try{await api("/api/auth/revoke-session",{token:s.token});row.remove();await refresh();}catch(e){message(e.message,true);}};row.append(label,revoke);$("sessions").append(row);}}catch(e){message(e.message,true);}};
(async()=>{try{config=await api("/api/config");$("github").hidden=!config.github;$("divider").hidden=!config.github;$("local").hidden=!config.local;$("email").value=hints.email||"";
  if(config.turnstileSiteKey){window.onPokachyTurnstile=()=>{widget=window.turnstile.render("#turnstile-container",{sitekey:config.turnstileSiteKey,action:"login",theme:"dark",callback:token=>{turnstileToken=token;},"expired-callback":()=>{turnstileToken="";},"error-callback":()=>{turnstileToken="";message("Verification failed. Please reload.",true);}});};const script=document.createElement("script");script.src="https://challenges.cloudflare.com/turnstile/v0/api.js?onload=onPokachyTurnstile&render=explicit";script.async=true;document.head.append(script);}
  await refresh();
}catch(e){$("loading").hidden=true;message(e.message,true);}})();
