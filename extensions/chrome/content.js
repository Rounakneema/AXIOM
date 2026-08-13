chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.action === "TRIGGER_INTENT_CHECK") {
    showIntentOverlay();
  }
});

let overlayExists = false;

function showIntentOverlay() {
  if (overlayExists) return;
  overlayExists = true;

  // Create the brutalist overlay
  const overlay = document.createElement("div");
  overlay.id = "axiom-intent-overlay";
  overlay.style.cssText = `
    position: fixed;
    top: 0; left: 0; width: 100vw; height: 100vh;
    background: rgba(0, 0, 0, 0.95);
    z-index: 2147483647;
    display: flex;
    flex-direction: column;
    justify-content: center;
    align-items: center;
    font-family: monospace;
    color: #ff3333;
    backdrop-filter: blur(10px);
  `;

  const title = document.createElement("h1");
  title.innerText = "STATE YOUR INTENT.";
  title.style.cssText = "font-size: 3rem; margin-bottom: 10px; text-transform: uppercase; letter-spacing: 2px;";

  const subtitle = document.createElement("p");
  subtitle.innerText = "AXIOM has flagged this domain as a distraction.\nWhy are we opening this?";
  subtitle.style.cssText = "font-size: 1.2rem; margin-bottom: 30px; text-align: center; color: #fff;";

  const input = document.createElement("input");
  input.type = "text";
  input.placeholder = "A valid, specific reason...";
  input.style.cssText = `
    width: 60%;
    max-width: 600px;
    padding: 15px;
    font-size: 1.5rem;
    font-family: monospace;
    background: #111;
    color: #0f0;
    border: 2px solid #333;
    outline: none;
    text-align: center;
  `;

  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      const reason = input.value.trim();
      
      // Basic validation for now (later we can send this to the AXIOM LLM for real evaluation)
      if (reason.length < 15 || reason.toLowerCase().includes("just checking") || reason.toLowerCase().includes("break")) {
        // Punish weak excuse
        input.value = "";
        input.placeholder = "WEAK EXCUSE. CLOSING TAB...";
        input.style.borderColor = "#f00";
        input.disabled = true;
        
        setTimeout(() => {
          chrome.runtime.sendMessage({ action: "CLOSE_TAB" });
        }, 1500);
      } else {
        // Accept valid excuse
        overlay.remove();
        overlayExists = false;
      }
    }
  });

  overlay.appendChild(title);
  overlay.appendChild(subtitle);
  overlay.appendChild(input);
  document.body.appendChild(overlay);

  setTimeout(() => input.focus(), 100);
}
