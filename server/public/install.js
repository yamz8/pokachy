const status = document.getElementById("copy-status");

async function copyText(value) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const text = document.createElement("textarea");
  text.className = "clipboard-fallback";
  text.value = value;
  text.setAttribute("readonly", "");
  document.body.append(text);
  text.select();
  const copied = document.execCommand("copy");
  text.remove();
  if (!copied) throw new Error("Copy failed");
}

for (const button of document.querySelectorAll(".copy-button")) {
  button.addEventListener("click", async () => {
    const command = button.dataset.copy;
    if (!command) return;
    try {
      await copyText(command);
      button.textContent = "Copied";
      status.textContent = "Command copied to your clipboard.";
      window.setTimeout(() => { button.textContent = "Copy"; }, 1800);
    } catch {
      status.textContent = "Couldn’t copy automatically. Select the command and copy it manually.";
    }
  });
}
