const $ = id => document.getElementById(id);
let config, state, widget, turnstileToken = "";
const userCode = new URLSearchParams(location.search).get("user_code");
let linkIntent = new URLSearchParams(location.search).get("link");
let changeIntent = new URLSearchParams(location.search).get("change");
const linkedProvider = new URLSearchParams(location.search).get("linked");
const linkError = new URLSearchParams(location.search).get("link_error");
const authError = new URLSearchParams(location.search).get("error") || new URLSearchParams(location.search).get("code");
const fragment = new URLSearchParams(location.hash.slice(1));
if (fragment.has("email")) sessionStorage.setItem("pokachy-hints", JSON.stringify(Object.fromEntries(fragment)));
const handoffStorageKey = "pokachy-account-handoff";
const fragmentHandoff = fragment.get("handoff"), fragmentAction = fragment.get("action");
if (/^[0-9a-f]{64}$/.test(fragmentHandoff||"") && ["account","github","email"].includes(fragmentAction)) {
  sessionStorage.setItem(handoffStorageKey, JSON.stringify({token:fragmentHandoff,action:fragmentAction}));
}
history.replaceState(null, "", location.pathname + location.search);
let hints = {}, pendingEmail = "";
try { hints = JSON.parse(sessionStorage.getItem("pokachy-hints") || "{}"); } catch { /* Optional hints only. */ }
function pendingHandoff() {
  try {
    const value=JSON.parse(sessionStorage.getItem(handoffStorageKey)||"null");
    return value&&/^[0-9a-f]{64}$/.test(value.token)&&["account","github","email"].includes(value.action)?value:null;
  } catch { return null; }
}
function message(text, bad=false) {
  const notice=$("notice");notice.replaceChildren();notice.className=bad?"error":"";notice.setAttribute("role",bad?"alert":"status");notice.setAttribute("aria-live",bad?"assertive":"polite");
  if(!text) return;
  const mark=document.createElement("span");mark.className="notice-mark";mark.setAttribute("aria-hidden","true");mark.textContent=bad?"!":"✳";
  const copy=document.createElement("span");copy.textContent=text;notice.append(mark,copy);
}
async function api(path, body, method=body===undefined?"GET":"POST", headers={}) {
  const response=await fetch(path,{method,headers:{"Content-Type":"application/json",...headers},body:body===undefined?undefined:JSON.stringify(body)});
  const data=await response.json();
  if(!response.ok) throw Object.assign(new Error(data.error?.message||data.error||data.message||"Please try again."),{status:response.status});
  return data;
}
function busy(form, callback) {
  form.addEventListener("submit",async e=>{e.preventDefault();const button=form.querySelector("button[type=submit]");button.disabled=true;message("");try{await callback();}catch(e){message(e.message,true);}finally{button.disabled=false;}});
}
function showProfile() {
  $("profile-form").hidden=false;$("handle").value=hints.handle||"";$("card-title").textContent="Claim your handle.";$("card-description").textContent="This is how friends will find and poke you.";$("handle").focus();
}
function showApproval(code) {
  $("approve").hidden=false;$("device-code").textContent=code;$("card-title").textContent="Connect this desktop.";$("card-description").textContent="Compare this code with the one shown in your terminal.";
}
function showConnected(handle) {
  renderAvatar(state.me);
  $("connected").hidden=false;$("identity").textContent=`@${handle}`;$("card-title").textContent="Send a tiny signal.";$("card-description").textContent="Your account is ready. Here are two ways to start.";$("link-github").hidden=!config.github||state.me.githubLinked;
  $("account-email").textContent=state.me.email||"";$("email-change-current-local").hidden=!config.local;$("email-change-new-local").hidden=!config.local;
  $("sessions").hidden=true;$("sessions-toggle").setAttribute("aria-expanded","false");$("sessions-icon").textContent="+";
  if(linkedProvider==="github"&&state.me.githubLinked) message("GitHub is connected to this Pokachy account.");
  else if(linkError==="github"||authError) message(authError==="email_does_not_match"||authError==="email_doesn't_match"?"That GitHub account uses a different email. Pokachy can link it when you explicitly try again.":"GitHub could not be connected. Please try again.",true);
  else if(linkIntent==="github"&&config.github&&!state.me.githubLinked){message(`Connect GitHub to @${handle}. Your Pokachy email will not change.`);$("link-github").focus();}
  if(changeIntent==="email") openEmailChange();
}
function openEmailChange() {
  $("email-change-panel").hidden=false;$("account-email-change").textContent="Cancel";
  $("email-change-current-form").hidden=false;$("email-change-new-form").hidden=true;
  $("new-account-email").focus();
}
function closeEmailChange() {
  $("email-change-panel").hidden=true;$("account-email-change").textContent="Change";pendingEmail="";
  $("email-change-current-form").reset();$("email-change-new-form").reset();
}
function clearChangeIntent() {
  changeIntent="";const url=new URL(location.href);url.searchParams.delete("change");history.replaceState(null,"",url.pathname+url.search);
}
function renderAvatar(person) {
  const initial=String(person.handle||person.name||"?").trim().replace(/^@/,"").charAt(0).toUpperCase()||"?";
  $("profile-initial").textContent=initial;
  const picture=$("profile-picture");picture.hidden=true;
  picture.onload=()=>{picture.hidden=false;};
  picture.onerror=()=>{picture.hidden=true;};
  try {
    const url=new URL(person.image);
    const custom=url.origin===location.origin&&url.pathname.startsWith("/avatars/");
    if((url.origin!=="https://avatars.githubusercontent.com"&&!custom)||url.username||url.password) throw new Error("Unsupported avatar");
    picture.src=url.href;
  } catch { picture.removeAttribute("src"); }
}
function sessionName(agent) {
  const value=typeof agent==="string"?agent:"";
  if(value.startsWith("Pokachy/")) return "Pokachy CLI";
  if(value.includes("Firefox/")) return "Firefox browser";
  if(value.includes("Edg/")) return "Edge browser";
  if(value.includes("Chrome/")) return "Chrome browser";
  if(value.includes("Safari/")) return "Safari browser";
  return value||"Pokachy device";
}
function renderSessions(items) {
  const list=$("sessions");list.replaceChildren();
  if(!items.length){const empty=document.createElement("p");empty.className="session-empty";empty.textContent="No signed-in devices found.";list.append(empty);return;}
  for(const session of items){const row=document.createElement("div");row.className="session";const info=document.createElement("span");info.className="session-info";const name=document.createElement("strong");name.textContent=sessionName(session.userAgent);const detail=document.createElement("small");const created=new Date(session.createdAt);detail.textContent=Number.isNaN(created.getTime())?"Active session":`Signed in ${created.toLocaleDateString(undefined,{month:"short",day:"numeric",year:"numeric"})}`;info.append(name,detail);const revoke=document.createElement("button");revoke.className="text-button session-revoke";revoke.textContent="Revoke";revoke.setAttribute("aria-label",`Revoke ${name.textContent}`);revoke.onclick=async()=>{try{revoke.disabled=true;message("");await api("/api/auth/revoke-session",{token:session.token});await refresh();message("Device access revoked.");}catch(e){message(e.message,true);}finally{revoke.disabled=false;}};row.append(info,revoke);list.append(row);}
}
function showFatal(error) {
  const unavailable=!!userCode&&error.message.startsWith("This device request is unavailable.");
  $("loading").hidden=true;for(const id of ["auth","profile-form","approve","connected"]) $(id).hidden=true;$("signout").hidden=true;$("fatal-actions").hidden=false;
  $("card-title").textContent=unavailable?"Code no longer available.":"We lost the signal.";
  $("card-description").textContent=unavailable?"This code may have expired or already been used.":"Pokachy couldn’t connect. Check your connection and try again.";
  $("recovery-label").textContent=unavailable?"Back to my account":"Try again";$("recovery-icon").textContent=unavailable?"←":"↻";$("recovery-button").dataset.action=unavailable?"account":"reload";
  message(unavailable?"Run pokachy init again to create a new code.":error.message,true);
}
async function refresh() {
  const session=await api("/api/auth/get-session");
  $("loading").hidden=true;
  $("auth").hidden=!!session;$("signout").hidden=!session;
  for(const id of ["profile-form","approve","connected"]) $(id).hidden=true;
  if(!session){$("card-title").textContent="Sign in";$("card-description").textContent="Use email or GitHub. No password needed.";return;}
  state=await api("/api/state");
  if(!state.me.handle){showProfile();return;}
  const handoff=pendingHandoff();
  if(handoff){
    try {
      await api("/api/account/handoff/claim",handoff);
      sessionStorage.removeItem(handoffStorageKey);
      if(handoff.action==="github") linkIntent="github";
      if(handoff.action==="email") changeIntent="email";
    } catch(e) {
      if(e.status!==409) sessionStorage.removeItem(handoffStorageKey);
      linkIntent="";changeIntent="";showConnected(state.me.handle);message(e.message,true);return;
    }
  }
  if(userCode){
    const device=await api(`/api/auth/device?user_code=${encodeURIComponent(userCode)}`);
    if(device.status!=="pending"||device.client_id!=="pokachy-cli") throw new Error("This device request is unavailable. Start pokachy init again.");
    showApproval(userCode);
  }
  else showConnected(state.me.handle);
}
busy($("email-form"),async()=>{
  if(!config.local&&!turnstileToken) throw new Error("Complete the verification first.");
  try{await api("/api/auth/email-otp/send-verification-otp",{email:$("email").value,type:"sign-in"},"POST",{"X-Turnstile-Token":turnstileToken});
    $("email-form").hidden=true;$("code-form").hidden=false;$("local-code").hidden=!config.local;
    $("card-title").textContent="Check your inbox.";$("card-description").textContent=`We sent a six-digit code to ${$("email").value}.`;
    $("auth-help-text").textContent="The code expires in 5 minutes. Check spam if it doesn’t arrive.";$("code").focus();message("");
  }finally{if(widget!==undefined){window.turnstile.reset(widget);turnstileToken="";}}
});
busy($("code-form"),async()=>{await api("/api/auth/sign-in/email-otp",{email:$("email").value,otp:$("code").value,name:hints.name||$("email").value.split("@")[0]});$("local-code").hidden=true;message("");await refresh();});
busy($("profile-form"),async()=>{await api("/api/profile",{handle:$("handle").value},"PUT");await refresh();});
$("account-email-change").onclick=()=>{$("email-change-panel").hidden?openEmailChange():(clearChangeIntent(),closeEmailChange());};
$("email-change-current-send").onclick=async()=>{const button=$("email-change-current-send");button.disabled=true;message("");try{await api("/api/account/email-change/current-code",{});message(`We sent a six-digit code to ${state.me.email}.`);$("email-change-current-code").focus();}catch(e){message(e.message,true);}finally{button.disabled=false;}};
$("email-change-current-local").onclick=async()=>{try{const d=await api(`/api/dev/mail?email=${encodeURIComponent(state.me.email)}`);$("email-change-current-code").value=d.otp||"";}catch(e){message(e.message,true);}};
busy($("email-change-current-form"),async()=>{pendingEmail=$("new-account-email").value.trim().toLowerCase();await api("/api/auth/email-otp/request-email-change",{newEmail:pendingEmail,otp:$("email-change-current-code").value});$("pending-account-email").textContent=pendingEmail;$("email-change-current-form").hidden=true;$("email-change-new-form").hidden=false;$("email-change-new-code").focus();message(`We sent a six-digit code to ${pendingEmail}.`);});
$("email-change-new-local").onclick=async()=>{try{const d=await api(`/api/dev/mail?email=${encodeURIComponent(pendingEmail)}`);$("email-change-new-code").value=d.otp||"";}catch(e){message(e.message,true);}};
busy($("email-change-new-form"),async()=>{await api("/api/auth/email-otp/change-email",{newEmail:pendingEmail,otp:$("email-change-new-code").value});clearChangeIntent();closeEmailChange();await refresh();message("Your account email has been changed and verified.");});
$("change-email").onclick=()=>{$("email-form").hidden=false;$("code-form").hidden=true;$("local-code").hidden=true;$("code").value="";$("card-title").textContent="Sign in";$("card-description").textContent="Use email or GitHub. No password needed.";$("auth-help-text").textContent="New account? We’ll create it when you sign in.";message("");$("email").focus();};
$("local-code").onclick=async()=>{try{const d=await api(`/api/dev/mail?email=${encodeURIComponent($("email").value)}`);$("code").value=d.otp||"";}catch(e){message(e.message,true);}};
async function github(link=false){try{const result=await api(link?"/api/auth/link-social":"/api/auth/sign-in/social",{provider:"github",callbackURL:link?location.origin+"/account?linked=github":location.origin+location.pathname+location.search,errorCallbackURL:link?location.origin+"/account?link_error=github":location.origin+"/account"});if(result.url) location.assign(result.url);}catch(e){message(e.message,true);}}
$("github").onclick=()=>github();$("link-github").onclick=()=>github(true);
$("signout").onclick=async()=>{try{await api("/api/auth/sign-out",{});location.assign("/account");}catch(e){message(e.message,true);}};
$("approve-button").onclick=async()=>{try{$("approve-button").disabled=true;await api("/api/auth/device/approve",{userCode});$("approve").hidden=true;showConnected(state.me.handle);$("card-title").textContent="See you on your desktop.";$("card-description").textContent="Your terminal is ready. You can close this tab.";message("Device connected. You can return to your terminal.");sessionStorage.removeItem("pokachy-hints");}catch(e){message(e.message,true);}finally{$("approve-button").disabled=false;}};
$("deny-button").onclick=async()=>{try{await api("/api/auth/device/deny",{userCode});$("approve").hidden=true;$("card-title").textContent="Request denied.";$("card-description").textContent="Nothing was connected. You can close this tab.";message("");}catch(e){message(e.message,true);}};
$("recovery-button").onclick=()=>{$("recovery-button").dataset.action==="account"?location.assign("/account"):location.reload();};
$("sessions-toggle").onclick=async()=>{const button=$("sessions-toggle"),list=$("sessions");if(button.getAttribute("aria-expanded")==="true"){list.hidden=true;button.setAttribute("aria-expanded","false");$("sessions-icon").textContent="+";return;}try{button.disabled=true;message("");renderSessions(await api("/api/auth/list-sessions"));list.hidden=false;button.setAttribute("aria-expanded","true");$("sessions-icon").textContent="−";}catch(e){message(e.message,true);}finally{button.disabled=false;}};
(async()=>{try{config=await api("/api/config");$("github").hidden=!config.github;$("divider").hidden=!config.github;$("local").hidden=!config.local;$("email").value=hints.email||"";
  if(config.turnstileSiteKey){window.onPokachyTurnstile=()=>{widget=window.turnstile.render("#turnstile-container",{sitekey:config.turnstileSiteKey,action:"login",theme:"dark",callback:token=>{turnstileToken=token;},"expired-callback":()=>{turnstileToken="";},"error-callback":()=>{turnstileToken="";message("Verification failed. Please reload.",true);}});};const script=document.createElement("script");script.src="https://challenges.cloudflare.com/turnstile/v0/api.js?onload=onPokachyTurnstile&render=explicit";script.async=true;document.head.append(script);}
  await refresh();
}catch(e){showFatal(e);}})();
