const status = document.getElementById("deletion-status");
const form = document.getElementById("deletion-form");
const reauth = document.getElementById("deletion-reauth");
const sendCode = document.getElementById("deletion-send-code");
const localCode = document.getElementById("deletion-local-code");
let accountEmail;
async function call(path, body) {
  const response = await fetch(path, { method: body === undefined ? "GET" : "POST", headers: { "Content-Type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body) });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error?.message || data.error || "Please try again.");
  return data;
}
reauth.onclick = async () => {
  reauth.disabled = true;
  try { await call("/api/auth/sign-out", {}); location.assign("/account"); }
  catch (error) { status.textContent = error.message; reauth.disabled = false; }
};
sendCode.onclick = async () => {
  sendCode.disabled = true;
  try {
    await call("/api/account/deletion-code", {});
    status.textContent = "Check your email for a six-digit code. It expires in five minutes.";
    const config = await call("/api/config");
    localCode.hidden = !config.local;
    document.getElementById("deletion-otp").focus();
  } catch (error) { status.textContent = error.message; }
  finally { sendCode.disabled = false; }
};
localCode.onclick = async () => {
  try {
    const data = await call(`/api/dev/mail?email=${encodeURIComponent(accountEmail)}`);
    document.getElementById("deletion-otp").value = data.otp || "";
  } catch (error) { status.textContent = error.message; }
};
form.addEventListener("submit", async event => {
  event.preventDefault();
  const button = form.querySelector("button[type=submit]"); button.disabled = true;
  status.textContent = "Deleting your account…";
  try {
    await call("/api/account/delete", { confirmation: document.getElementById("deletion-confirmation").value, otp: document.getElementById("deletion-otp").value });
    sessionStorage.removeItem("pokachy-hints");
    form.hidden = true; reauth.hidden = true;
    status.textContent = "Your account has been deleted and all devices have lost access. Run pokachy logout on each computer to clear its saved data.";
  } catch (error) { status.textContent = error.message; status.setAttribute("role", "alert"); }
  finally { button.disabled = false; }
});
(async () => {
  try {
    const identity = await call("/api/auth/get-session");
    if (!identity) {
      status.textContent = "Sign in before deleting your account.";
      document.getElementById("deletion-signin").hidden = false; return;
    }
    accountEmail = identity.user.email;
    status.textContent = `Signed in as ${identity.user.email}. Deletion requires a sign-in within the last five minutes and an email code.`;
    reauth.hidden = false;
    const age = Date.now() - new Date(identity.session.createdAt).getTime();
    form.hidden = !Number.isFinite(age) || age < 0 || age > 300000;
  } catch (error) { status.textContent = error.message; status.setAttribute("role", "alert"); }
})();
