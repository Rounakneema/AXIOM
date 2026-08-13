let currentTabId = null;
let sessionStartTime = Date.now();
const API_URL = "http://127.0.0.1:4444/api/browser-event";

function getDomain(url) {
  try {
    return new URL(url).hostname;
  } catch (e) {
    return "";
  }
}

async function sendEvent(tab, activityType, duration = 0) {
  if (!tab || !tab.url || tab.url.startsWith("chrome://") || tab.url.startsWith("edge://")) {
    return; // Ignore internal pages
  }

  const payload = {
    url: tab.url,
    title: tab.title || "",
    domain: getDomain(tab.url),
    app: "chrome.exe", // Fallback, though we assume Chrome/Edge based on extension
    activity: activityType,
    duration: duration
  };

  try {
    const response = await fetch(API_URL, {
      method: "POST",
      headers: {
        "Content-Type": "application/json"
      },
      body: JSON.stringify(payload)
    });
    
    if (activityType === "foreground_interactive" && duration === 0) {
        const data = await response.json();
        // If AXIOM tags this as entertainment, trigger intent checker
        if (data.trigger_intent_check) {
            chrome.tabs.sendMessage(tab.id, { action: "TRIGGER_INTENT_CHECK" }).catch(err => console.log("Content script not ready"));
        }
    }
  } catch (err) {
    console.error("AXIOM API unreachable:", err);
  }
}

// When a new tab becomes active
chrome.tabs.onActivated.addListener(async (activeInfo) => {
  // Close previous session
  if (currentTabId !== null) {
    try {
      const prevTab = await chrome.tabs.get(currentTabId);
      const duration = Date.now() - sessionStartTime;
      await sendEvent(prevTab, "background_passive", duration);
    } catch (e) {
      // Tab might be closed
    }
  }

  // Start new session
  currentTabId = activeInfo.tabId;
  sessionStartTime = Date.now();
  
  try {
    const newTab = await chrome.tabs.get(currentTabId);
    await sendEvent(newTab, "foreground_interactive", 0);
  } catch (e) {}
});

// When the active tab updates its URL/Title
chrome.tabs.onUpdated.addListener(async (tabId, changeInfo, tab) => {
  if (tabId === currentTabId && tab.url && tab.status === "complete") {
    // Reset session for the new URL on the same tab
    const duration = Date.now() - sessionStartTime;
    await sendEvent(tab, "foreground_interactive", duration);
    sessionStartTime = Date.now();
  }
});

// Listen for intent checker deciding to close the tab
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.action === "CLOSE_TAB" && sender.tab) {
    chrome.tabs.remove(sender.tab.id);
  }
});
