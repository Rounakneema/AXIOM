import javax.swing.*;
import java.awt.*;
import java.awt.event.*;
import java.awt.geom.*;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.Random;

/**
 * AXIOM Mascot v3 — Compact Desktop Pet with Click-to-Dashboard
 * Small, non-distracting, screen-aware, feature-rich.
 */
public class MascotOverlay extends JWindow {

    private static final String API_URL = "http://127.0.0.1:4444/api/state";
    private int failCount = 0;

    // ── State Machine ──
    enum State { IDLE, WALKING, RUNNING, JUMPING, LANDING, DANCING, SITTING, SLEEPING, WAVING, ANGRY_STOMP, WORKING, RAGE_MODE }
    private State state = State.IDLE;
    private State prevLoggedState = null; // For logging state transitions

    private final HttpClient client;
    private final Random rng = new Random();
    private final int screenW, screenH;
    private static final int PET_SIZE = 3; // pixel scale (16*3 = 48px sprite)
    private static final int WIN_W = 110;
    private static final int WIN_H = 130;

    // ── Physics ──
    private double posX, posY;
    private double velX = 0, velY = 0;
    private double targetX;
    private boolean onGround = true;
    private boolean facingRight = true;
    private boolean isDragging = false;
    private int petDragX = 0;
    private int petDragY = 0;
    private int lastScreenX = -1;
    private int lastScreenY = -1;

    // ── Timers ──
    private int animTick = 0;
    private int roamCooldown = 60;
    private int stateTicks = 0;
    private int idleTicks = 0;

    // ── API Data ──
    private double focusScore = 100;
    private int codingMinutes = 0;
    private int entertainmentMinutes = 0;
    private int musicMinutes = 0;
    private String mood = "Neutral";
    private String llmMessage = "";
    private String bubbleText = "";
    private int bubbleTimer = 0;

    // ── Dashboard ──
    private DashboardPanel dashboard = null;

    // ══════════════════════════════════════════════════
    //  CONSTRUCTOR
    // ══════════════════════════════════════════════════
    public MascotOverlay() {
        setAlwaysOnTop(true);
        setType(Type.UTILITY); // Hidden from taskbar
        setBackground(new Color(0, 0, 0, 0));
        setFocusableWindowState(false);

        Dimension screen = Toolkit.getDefaultToolkit().getScreenSize();
        screenW = screen.width;
        screenH = screen.height;

        setSize(WIN_W, WIN_H);

        // Start near bottom-right
        posX = screenW - 250;
        posY = getGroundY();
        targetX = posX;
        setLocation((int) posX, (int) posY);

        setContentPane(new RenderPanel());

        // ── Mouse: Drag + Click ──
        MouseAdapter mouse = new MouseAdapter() {
            int lx, ly;
            boolean moved = false;

            public void mousePressed(MouseEvent e) {
                if (SwingUtilities.isRightMouseButton(e)) {
                    if (llmMessage != null && !llmMessage.isEmpty() && !llmMessage.equals("Awaiting data.")) {
                        showBubble(llmMessage);
                    } else {
                        showBubble("Hmm... I'm watching you.");
                    }
                    return;
                }
                
                if (state == State.RAGE_MODE) {
                    state = State.ANGRY_STOMP; // Dismiss the hijack
                    stateTicks = 0;
                    setAlwaysOnTop(false);
                    return;
                }

                lx = e.getXOnScreen();
                ly = e.getYOnScreen();
                petDragX = e.getX();
                petDragY = e.getY();
                moved = false;
                isDragging = true;
                velX = 0;
                velY = 0;
                lastScreenX = lx;
                lastScreenY = ly;
            }

            public void mouseDragged(MouseEvent e) {
                if (!isDragging || SwingUtilities.isRightMouseButton(e)) return;
                int currX = e.getXOnScreen();
                int currY = e.getYOnScreen();
                
                if (lastScreenX != -1) {
                    double dx = currX - lastScreenX;
                    double dy = currY - lastScreenY;
                    if (Math.abs(dx) > 2 || Math.abs(dy) > 2) {
                        velX = dx * 0.8;
                        velY = dy * 0.8;
                    }
                }
                lastScreenX = currX;
                lastScreenY = currY;

                posX = currX - petDragX;
                posY = currY - petDragY;
                onGround = false;
                targetX = posX;
                
                if (state == State.SLEEPING || state == State.SITTING || state == State.WORKING) {
                    state = State.IDLE;
                    idleTicks = 0;
                }
                setLocation((int) posX, (int) posY);
                moved = true;
            }

            public void mouseReleased(MouseEvent e) {
                if (SwingUtilities.isRightMouseButton(e)) return;
                isDragging = false;
                lastScreenX = -1;
                
                if (!moved) {
                    toggleDashboard();
                    if (state != State.WAVING) {
                        state = State.WAVING;
                        stateTicks = 0;
                    }
                } else {
                    // Cap throw velocity
                    if (velX > 40) velX = 40;
                    if (velX < -40) velX = -40;
                    if (velY > 40) velY = 40;
                    if (velY < -40) velY = -40;
                }
            }
        };
        addMouseListener(mouse);
        addMouseMotionListener(mouse);

        client = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(2)).build();

        // ── Main Loop (30 FPS) ──
        System.out.println("[MASCOT] 🎮 Engine started. Screen: " + screenW + "x" + screenH + " | GroundY: " + (int)getGroundY() + " | StartPos: (" + (int)posX + ", " + (int)posY + ")");
        new javax.swing.Timer(33, e -> {
            animTick++;
            stateTicks++;
            if (!isDragging) updatePhysics();
            updateBehavior();
            if (animTick % 90 == 0) pollAxiomState();
            // Log state transitions
            if (state != prevLoggedState) {
                System.out.println("[MASCOT] ▶ State: " + prevLoggedState + " → " + state + " | Pos: (" + (int)posX + ", " + (int)posY + ") | Facing: " + (facingRight ? "→" : "←") + " | IdleTicks: " + idleTicks);
                prevLoggedState = state;
            }
            // Periodic position log every 5 seconds (150 ticks)
            if (animTick % 150 == 0) {
                System.out.println("[MASCOT] 📍 Tick=" + animTick + " State=" + state + " Pos=(" + (int)posX + "," + (int)posY + ") Vel=(" + String.format("%.1f",velX) + "," + String.format("%.1f",velY) + ") OnGround=" + onGround + " Focus=" + String.format("%.1f", focusScore) + "%");
            }
            setLocation((int) posX, (int) posY);
            repaint();
        }).start();
    }

    private double getGroundY() {
        // Use actual usable screen area (excludes taskbar)
        Rectangle usable = GraphicsEnvironment.getLocalGraphicsEnvironment().getMaximumWindowBounds();
        return usable.y + usable.height - WIN_H;
    }

    // ══════════════════════════════════════════════════
    //  PHYSICS
    // ══════════════════════════════════════════════════
    private void updatePhysics() {
        double groundY = getGroundY();

        if (!onGround) {
            velY += 1.2; // stronger gravity for throwing
            posY += velY;
            posX += velX;

            // Bounce off walls
            if (posX < 0) {
                posX = 0;
                velX = -velX * 0.7; // bounce
                facingRight = true;
            } else if (posX > screenW - WIN_W) {
                posX = screenW - WIN_W;
                velX = -velX * 0.7; // bounce
                facingRight = false;
            }

            if (posY >= groundY) {
                posY = groundY;
                if (velY > 5) {
                    velY = -velY * 0.4; // floor bounce
                } else {
                    velY = 0;
                    onGround = true;
                    if (state == State.JUMPING) {
                        state = State.LANDING;
                        stateTicks = 0;
                    } else if (state != State.RAGE_MODE) {
                        state = State.IDLE;
                        idleTicks = 0;
                    }
                }
            }
        } else {
            posY = groundY;
            if (state == State.WALKING || state == State.RUNNING) {
                double dx = targetX - posX;
                double speed = (state == State.RUNNING) ? 3.5 : 1.5;
                if (Math.abs(dx) < speed + 1) {
                    velX = 0;
                    state = State.IDLE;
                    idleTicks = 0;
                } else {
                    velX = Math.signum(dx) * speed;
                    facingRight = (dx > 0);
                    posX += velX;
                }
            } else if (Math.abs(velX) > 0.1) {
                velX *= 0.8; // friction
                posX += velX;
                if (posX < 0) { posX = 0; velX = -velX * 0.5; }
                if (posX > screenW - WIN_W) { posX = screenW - WIN_W; velX = -velX * 0.5; }
            } else {
                velX = 0;
            }
        }
    }

    // ══════════════════════════════════════════════════
    //  BEHAVIOR AI
    // ══════════════════════════════════════════════════
    private void updateBehavior() {
        if (bubbleTimer > 0) bubbleTimer--;
        roamCooldown--;

        // Landing recovery (brief squish then idle)
        if (state == State.LANDING && stateTicks > 8) {
            state = State.IDLE;
            idleTicks = 0;
        }

        // Wave recovery
        if (state == State.WAVING && stateTicks > 40) {
            state = State.IDLE;
            idleTicks = 0;
        }

        // Angry stomp recovery
        if (state == State.ANGRY_STOMP && stateTicks > 50) {
            state = State.IDLE;
            idleTicks = 0;
        }

        // Idle progression: idle -> sit -> sleep
        if (state == State.IDLE) {
            idleTicks++;
            if (idleTicks > 300) { state = State.SITTING; stateTicks = 0; System.out.println("[MASCOT] 🪑 Sitting down (idleTicks=" + idleTicks + ")"); }
        }
        if (state == State.SITTING) {
            idleTicks++;
            if (idleTicks > 500) { state = State.SLEEPING; stateTicks = 0; System.out.println("[MASCOT] 😴 Falling asleep (idleTicks=" + idleTicks + ")"); }
        }

        // Random Muttering
        if (stateTicks % 500 == 0 && focusScore < 80 && state != State.RAGE_MODE && bubbleTimer <= 0) {
            String[] mutters = {"I'm bored...", "Shouldn't you be coding?", "PipelineForge needs you!", "Stop slacking...", "I am judging you.", "Back to work!"};
            showBubble(mutters[rng.nextInt(mutters.length)]);
        }

        // Random actions
        if (roamCooldown <= 0 && onGround && (state == State.IDLE || state == State.SITTING || state == State.SLEEPING || state == State.WORKING)) {
            roamCooldown = 120 + rng.nextInt(200);

            // Don't do actions too often when sleeping
            if (state == State.SLEEPING && rng.nextInt(100) < 70) return;
            if (state == State.SITTING && rng.nextInt(100) < 40) return;

            int action = rng.nextInt(100);

            if (action < 30) {
                // Walk to nearby spot (screen-proportional, max 1/4 screen width)
                int range = screenW / 4;
                targetX = posX + rng.nextInt(range) - range / 2;
                targetX = Math.max(20, Math.min(targetX, screenW - WIN_W - 20));
                state = State.WALKING;
                idleTicks = 0;
                System.out.println("[MASCOT] 🚶 WALK → targetX=" + (int)targetX + " from posX=" + (int)posX + " (roll=" + action + ")");
            } else if (action < 42) {
                // Run to distant spot
                targetX = 30 + rng.nextInt(screenW - 100);
                state = State.RUNNING;
                idleTicks = 0;
                System.out.println("[MASCOT] 🏃 RUN → targetX=" + (int)targetX + " from posX=" + (int)posX + " (roll=" + action + ")");
            } else if (action < 55) {
                // Small hop
                velY = -8;
                onGround = false;
                state = State.JUMPING;
                idleTicks = 0;
                System.out.println("[MASCOT] 🦘 JUMP! velY=-8 (roll=" + action + ")");
            } else if (action < 70) {
                // Dance
                state = State.DANCING;
                stateTicks = 0;
                idleTicks = 0;
                System.out.println("[MASCOT] 💃 DANCE! (roll=" + action + ")");
            } else {
                // Just wake up
                state = State.IDLE;
                idleTicks = 0;
                System.out.println("[MASCOT] 😴→😐 Wake up (roll=" + action + ")");
            }
        }

        // End dance after ~2.5 seconds
        if (state == State.DANCING && stateTicks > 75) {
            state = State.IDLE;
            idleTicks = 0;
        }
    }

    // ══════════════════════════════════════════════════
    //  API POLLING
    // ══════════════════════════════════════════════════
    private void showBubble(String text) {
        if (text == null || text.isEmpty()) return;
        // Truncate for display
        bubbleText = text.length() > 60 ? text.substring(0, 57) + "..." : text;
        bubbleTimer = 600;

        // Wake up if sleeping
        if (state == State.SLEEPING || state == State.SITTING) {
            state = State.IDLE;
            idleTicks = 0;
        }
    }

    private void pollAxiomState() {
        HttpRequest req = HttpRequest.newBuilder()
                .uri(URI.create(API_URL))
                .timeout(Duration.ofSeconds(2))
                .GET().build();

        client.sendAsync(req, HttpResponse.BodyHandlers.ofString())
            .thenAccept(resp -> {
                if (resp.statusCode() == 200) {
                    failCount = 0;
                    System.out.println("[MASCOT] 📡 API OK → " + resp.body().substring(0, Math.min(resp.body().length(), 120)));
                    parseState(resp.body());
                } else {
                    System.out.println("[MASCOT] ⚠️ API returned status: " + resp.statusCode());
                }
            })
            .exceptionally(ex -> {
                failCount++;
                System.out.println("[MASCOT] ❌ API poll failed (" + failCount + "): " + ex.getMessage());
                // Removed System.exit(0) to allow mascot to survive API restarts
                return null;
            });
    }

    /** Simple JSON value extractor — handles the fixed API format reliably */
    private void parseState(String json) {
        try {
            double oldFocus = focusScore;
            focusScore = extractDouble(json, "focus_score", 100);
            codingMinutes = (int) extractDouble(json, "coding_minutes", 0);
            entertainmentMinutes = (int) extractDouble(json, "entertainment_minutes", 0);
            musicMinutes = (int) extractDouble(json, "music_minutes", 0);
            mood = extractString(json, "mood", "Neutral");
            String msg = extractString(json, "message", "");

            // Gamification States
            if (focusScore < 40 && state != State.RAGE_MODE && state != State.ANGRY_STOMP) {
                state = State.RAGE_MODE;
                // Hijack center of screen!
                posX = screenW / 2 - WIN_W / 2;
                posY = screenH / 2 - WIN_H / 2;
                velX = 0; velY = 0; onGround = false;
                setAlwaysOnTop(true);
            } else if (focusScore >= 95 && state != State.WORKING && state != State.WAVING && state != State.WALKING && state != State.RUNNING) {
                state = State.WORKING;
                stateTicks = 0;
            } else if (focusScore >= 40 && state == State.RAGE_MODE) {
                state = State.IDLE;
            }

            // Show LLM roast if it changed
            if (!msg.equals("Awaiting data.") && !msg.isEmpty() && !msg.equals(llmMessage)) {
                llmMessage = msg;
                showBubble(msg);
            }

            // Update dashboard if open
            if (dashboard != null && dashboard.isVisible()) {
                dashboard.repaint();
            }
        } catch (Exception e) {
            // Silently ignore parse errors — don't crash
        }
    }

    private double extractDouble(String json, String key, double fallback) {
        int idx = json.indexOf("\"" + key + "\"");
        if (idx < 0) return fallback;
        int colon = json.indexOf(':', idx);
        if (colon < 0) return fallback;
        int end = colon + 1;
        while (end < json.length() && json.charAt(end) != ',' && json.charAt(end) != '}') end++;
        try { return Double.parseDouble(json.substring(colon + 1, end).trim()); }
        catch (Exception e) { return fallback; }
    }

    private String extractString(String json, String key, String fallback) {
        int idx = json.indexOf("\"" + key + "\"");
        if (idx < 0) return fallback;
        int colon = json.indexOf(':', idx);
        if (colon < 0) return fallback;
        int start = json.indexOf('"', colon);
        if (start < 0) return fallback;
        int end = json.indexOf('"', start + 1);
        if (end < 0) return fallback;
        return json.substring(start + 1, end);
    }

    // ══════════════════════════════════════════════════
    //  DASHBOARD POPUP
    // ══════════════════════════════════════════════════
    private void toggleDashboard() {
        if (dashboard != null && dashboard.isVisible()) {
            dashboard.setVisible(false);
            dashboard.dispose();
            dashboard = null;
            return;
        }

        dashboard = new DashboardPanel();
        // Position above the mascot
        int dx = (int) posX - 80;
        int dy = (int) posY - 220;
        if (dx < 10) dx = 10;
        if (dx + 260 > screenW) dx = screenW - 270;
        if (dy < 10) dy = (int) posY + WIN_H + 10;
        dashboard.setLocation(dx, dy);
        dashboard.setVisible(true);
    }

    class DashboardPanel extends JWindow {
        DashboardPanel() {
            setAlwaysOnTop(true);
            setSize(260, 200);
            setBackground(new Color(0, 0, 0, 0));

            // Close when clicking outside
            addWindowFocusListener(new WindowAdapter() {
                public void windowLostFocus(WindowEvent e) {
                    setVisible(false);
                    dispose();
                    dashboard = null;
                }
            });

            setFocusableWindowState(true);
            setContentPane(new JPanel() {
                { setOpaque(false); }
                @Override
                protected void paintComponent(Graphics g) {
                    super.paintComponent(g);
                    Graphics2D g2 = (Graphics2D) g.create();
                    g2.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON);

                    // Dark glass background
                    g2.setColor(new Color(18, 18, 24, 230));
                    g2.fill(new RoundRectangle2D.Float(0, 0, getWidth(), getHeight(), 16, 16));

                    // Border glow
                    g2.setColor(new Color(80, 80, 120, 80));
                    g2.setStroke(new BasicStroke(1.5f));
                    g2.draw(new RoundRectangle2D.Float(1, 1, getWidth() - 2, getHeight() - 2, 14, 14));

                    int y = 22;
                    // Title
                    g2.setFont(new Font("Segoe UI", Font.BOLD, 13));
                    g2.setColor(new Color(180, 180, 220));
                    g2.drawString("AXIOM Dashboard", 16, y);
                    y += 6;

                    // Divider
                    g2.setColor(new Color(60, 60, 80));
                    g2.drawLine(16, y, getWidth() - 16, y);
                    y += 18;

                    // Focus Score (big)
                    Color scoreColor = (focusScore >= 70) ? new Color(0, 220, 120) :
                                       (focusScore >= 40) ? new Color(255, 200, 0) : new Color(255, 60, 60);
                    g2.setFont(new Font("Segoe UI", Font.BOLD, 28));
                    g2.setColor(scoreColor);
                    g2.drawString(String.format("%.0f%%", focusScore), 16, y);

                    g2.setFont(new Font("Segoe UI", Font.PLAIN, 10));
                    g2.setColor(new Color(120, 120, 150));
                    g2.drawString("FOCUS SCORE", 90, y - 12);
                    g2.drawString("Mood: " + mood, 90, y);
                    y += 24;

                    // Stats row
                    g2.setFont(new Font("Segoe UI", Font.BOLD, 11));
                    g2.setColor(new Color(0, 200, 255));
                    g2.drawString("\u2580 Coding", 16, y);
                    g2.setColor(new Color(160, 170, 200));
                    g2.drawString(codingMinutes + " min", 75, y);

                    g2.setColor(new Color(255, 100, 100));
                    g2.drawString("\u2580 Distracted", 125, y);
                    g2.setColor(new Color(160, 170, 200));
                    g2.drawString(entertainmentMinutes + " min", 205, y);
                    y += 16;
                    
                    g2.setColor(new Color(200, 100, 255));
                    g2.drawString("\u2580 Music", 16, y);
                    g2.setColor(new Color(160, 170, 200));
                    g2.drawString(musicMinutes + " min", 75, y);
                    y += 22;

                    // Focus bar
                    int barW = getWidth() - 32;
                    g2.setColor(new Color(40, 40, 55));
                    g2.fillRoundRect(16, y, barW, 8, 4, 4);
                    int fillW = (int) ((focusScore / 100.0) * barW);
                    g2.setColor(scoreColor);
                    g2.fillRoundRect(16, y, Math.max(fillW, 4), 8, 4, 4);
                    y += 20;

                    // LLM Message
                    if (llmMessage != null && !llmMessage.isEmpty() && !llmMessage.equals("Awaiting data.")) {
                        g2.setColor(new Color(60, 60, 80));
                        g2.drawLine(16, y - 6, getWidth() - 16, y - 6);
                        g2.setFont(new Font("Segoe UI", Font.ITALIC, 9));
                        g2.setColor(new Color(200, 180, 140));
                        // Word wrap
                        String display = llmMessage.length() > 80 ? llmMessage.substring(0, 77) + "..." : llmMessage;
                        drawWrappedText(g2, "\"\u200b" + display + "\"", 16, y + 4, getWidth() - 32);
                    }

                    g2.dispose();
                }

                private void drawWrappedText(Graphics2D g2, String text, int x, int y, int maxW) {
                    FontMetrics fm = g2.getFontMetrics();
                    StringBuilder line = new StringBuilder();
                    int lineY = y;
                    for (String word : text.split(" ")) {
                        if (fm.stringWidth(line + word + " ") > maxW && line.length() > 0) {
                            g2.drawString(line.toString(), x, lineY);
                            lineY += fm.getHeight();
                            line = new StringBuilder();
                        }
                        line.append(word).append(" ");
                    }
                    if (line.length() > 0) g2.drawString(line.toString(), x, lineY);
                }
            });
        }
    }

    // ══════════════════════════════════════════════════
    //  SPRITE ENGINE
    // ══════════════════════════════════════════════════
    class RenderPanel extends JPanel {
        RenderPanel() { setOpaque(false); }

        //  16x15 sprites — B=outline, W=body, C=eyes, G=gray, H=hand
        String[] sprIdle = {
            "......BBBB......",
            "....BBWWWWBB....",
            "...BWGGWWGGWB...",
            "..BWWCCWWCCWWB..",
            "..BWWWWMMWWWWB..",
            "...BWWWWWWWWB...",
            "....BBBBBBBB....",
            "...HBWWWWWWBH...",
            "..B.BWWWWWWB.B..",
            "..B.BWWWWWWB.B..",
            "....BWWWWWWB....",
            "....BBBBBBBB....",
            "....BB....BB....",
            "....BB....BB....",
            "...BBB....BBB..."
        };

        String[] sprWalk1 = {
            "......BBBB......",
            "....BBWWWWBB....",
            "...BWGGWWGGWB...",
            "..BWWCCWWCCWWB..",
            "..BWWWWMMWWWWB..",
            "...BWWWWWWWWB...",
            "....BBBBBBBB....",
            "..BBBWWWWWWBH...",
            "..B.BWWWWWWB.B..",
            "....BWWWWWWB....",
            "....BWWWWWWB....",
            "....BBBBBBBB....",
            ".......BB.BB....",
            ".......BB.BB....",
            ".......BB.BBB..."
        };

        String[] sprWalk2 = {
            "......BBBB......",
            "....BBWWWWBB....",
            "...BWGGWWGGWB...",
            "..BWWCCWWCCWWB..",
            "..BWWWWMMWWWWB..",
            "...BWWWWWWWWB...",
            "....BBBBBBBB....",
            "...HBWWWWWWBBB..",
            "..B.BWWWWWWB.B..",
            "....BWWWWWWB....",
            "....BWWWWWWB....",
            "....BBBBBBBB....",
            "....BB.BB.......",
            "....BB.BB.......",
            "...BBB.BB......."
        };

        String[] sprDance1 = {
            "......BBBB......",
            "....BBWWWWBB....",
            "...BWGGWWGGWB...",
            "..BWWC>WWC>WWB..",
            "..BWWWWMMWWWWB..",
            "...BWWWWWWWWB...",
            "....BBBBBBBB....",
            ".BB.BWWWWWWB.BB.",
            ".B..BWWWWWWB..B.",
            "BB..BWWWWWWB..BB",
            "....BWWWWWWB....",
            "....BBBBBBBB....",
            "....BB....BB....",
            "...BBB....BB....",
            "...BBB...BBB...."
        };

        String[] sprDance2 = {
            "................",
            "......BBBB......",
            "....BBWWWWBB....",
            "...BWGGWWGGWB...",
            "..BWWC<WWC<WWB..",
            "..BWWWWMMWWWWB..",
            "...BWWWWWWWWB...",
            "....BBBBBBBB....",
            "...HBWWWWWWBH...",
            "..B.BWWWWWWB.B..",
            "....BWWWWWWB....",
            "....BBBBBBBB....",
            "....BB....BB....",
            "....BB...BBB....",
            "...BBB...BBB...."
        };

        String[] sprSit = {
            "................",
            "................",
            "................",
            "......BBBB......",
            "....BBWWWWBB....",
            "...BWGGWWGGWB...",
            "..BWWCCWWCCWWB..",
            "..BWWWWMMWWWWB..",
            "...BWWWWWWWWB...",
            "....BBBBBBBB....",
            "...HBWWWWWWBH...",
            "..B.BWWWWWWB.B..",
            "....BBWWWWBB....",
            "....BBBBBBBB....",
            "....BB....BB...."
        };

        String[] sprSleep = {
            "................",
            "................",
            "................",
            "......BBBB......",
            "....BBWWWWBB....",
            "...BWGGWWGGWB...",
            "..BWW--WW--WWB..",
            "..BWWWWMMWWWWB..",
            "...BWWWWWWWWB...",
            "....BBBBBBBB....",
            "...HBWWWWWWBH...",
            "..B.BWWWWWWB.B..",
            "....BBWWWWBB....",
            "....BBBBBBBB....",
            "....BB....BB...."
        };

        String[] sprWave = {
            "......BBBB......",
            "....BBWWWWBB....",
            "...BWGGWWGGWB...",
            "..BWWC>WWC>WWB..",
            "..BWWWWMMWWWWB..",
            "...BWWWWWWWWB...",
            "....BBBBBBBB....",
            "...HBWWWWWWB.BB.",
            "..B.BWWWWWWB..B.",
            "....BWWWWWWB..BB",
            "....BWWWWWWB....",
            "....BBBBBBBB....",
            "....BB....BB....",
            "....BB....BB....",
            "...BBB....BBB..."
        };

        String[] sprAngry = {
            "......BBBB......",
            "....BBWWWWBB....",
            "...BWBBWWBBWB...",
            "..BWWRRWWRRWWB..",
            "..BWWWWMMWWWWB..",
            "...BWWWWWWWWB...",
            "....BBBBBBBB....",
            ".BB.BWWWWWWB.BB.",
            ".B..BWWWWWWB..B.",
            "BB..BWWWWWWB..BB",
            "....BWWWWWWB....",
            "....BBBBBBBB....",
            "...BBB....BBB...",
            "...BBB....BBB...",
            "..BBBB....BBBB.."
        };

        String[] sprLand = {
            "................",
            "......BBBB......",
            "....BBWWWWBB....",
            "...BWGGWWGGWB...",
            "..BWWCCWWCCWWB..",
            "..BWWWWMMWWWWB..",
            "...BWWWWWWWWB...",
            "....BBBBBBBB....",
            "..BBBWWWWWWBBB..",
            "..B.BWWWWWWB.B..",
            "....BWWWWWWB....",
            "....BBBBBBBB....",
            "...BBB....BBB...",
            "...BBB....BBB...",
            "..BBBB....BBBB.."
        };

        String[] sprWork = {
            "......BBBB......",
            "....BBWWWWBB....",
            "...BWGGWWGGWB...",
            "..BWWCCWWCCWWB..",
            "..BWWWWMMWWWWB..",
            "...BWWWWWWWWB...",
            "....BBBBBBBB....",
            "...BBWWWWWWBB...",
            "...B.BWWWWW.B...",
            "...B.BWWWWW.B...",
            ".....BBBBBB.....",
            "..BBBBBBBBBBBB..",
            ".BLLLLLLLLLLLLB.",
            ".BLLLLLLLLLLLLB.",
            "..BBBBBBBBBBBB.."
        };

        @Override
        protected void paintComponent(Graphics g) {
            super.paintComponent(g);
            Graphics2D g2 = (Graphics2D) g.create();

            String[] sprite = sprIdle;

            switch (state) {
                case WALKING:
                    sprite = (animTick % 14 < 7) ? sprWalk1 : sprWalk2;
                    break;
                case RUNNING:
                    sprite = (animTick % 6 < 3) ? sprWalk1 : sprWalk2;
                    break;
                case DANCING:
                    sprite = (animTick % 10 < 5) ? sprDance1 : sprDance2;
                    break;
                case JUMPING:
                    sprite = sprDance1;
                    break;
                case LANDING:
                    sprite = sprLand;
                    break;
                case SITTING:
                    sprite = sprSit;
                    break;
                case SLEEPING:
                    sprite = sprSleep;
                    break;
                case WAVING:
                    sprite = (animTick % 8 < 4) ? sprWave : sprIdle;
                    break;
                case ANGRY_STOMP:
                    sprite = (animTick % 6 < 3) ? sprAngry : sprLand;
                    break;
            }

            int spriteW = 16 * PET_SIZE;
            int spriteH = sprite.length * PET_SIZE;
            int drawX = WIN_W / 2 - spriteW / 2;
            int drawY = WIN_H - spriteH - 16;

            // Breathing bob for idle
            if (state == State.IDLE) {
                drawY += (int)(Math.sin(animTick * 0.08) * 1.5);
            }

            // Flip sprite if facing left
            if (!facingRight) {
                g2.translate(WIN_W, 0);
                g2.scale(-1, 1);
            }

            // Draw sprite
            for (int r = 0; r < sprite.length; r++) {
                for (int c = 0; c < sprite[r].length(); c++) {
                    char p = sprite[r].charAt(c);
                    if (p == '.') continue;

                    Color color;
                    switch (p) {
                        case 'B': color = new Color(20, 20, 28); break;
                        case 'W': color = new Color(240, 240, 248); break;
                        case 'G': color = new Color(180, 180, 195); break;
                        case 'H': color = new Color(20, 20, 28); break;
                        case 'M': color = new Color(160, 160, 175); break; // mouth
                        case 'R': color = new Color(255, 50, 50); break; // angry eyes
                        case '-': color = new Color(100, 100, 130); break; // closed eyes
                        case 'C': case '<': case '>':
                            color = focusScore < 30 ? new Color(255, 50, 50) : new Color(0, 190, 255);
                            break;
                        default: color = Color.MAGENTA;
                    }

                    g2.setColor(color);
                    g2.fillRect(drawX + c * PET_SIZE, drawY + r * PET_SIZE, PET_SIZE, PET_SIZE);
                }
            }

            g2.dispose(); // Release flipped context

            // Draw non-flipped overlays with the original graphics
            Graphics2D gUI = (Graphics2D) g;
            gUI.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON);

            // Zzz for sleeping (drawn un-flipped so text is readable)
            if (state == State.SLEEPING) {
                gUI.setColor(new Color(140, 140, 200, 180));
                gUI.setFont(new Font("Monospaced", Font.BOLD, 10));
                int zOff = (int)(Math.sin(animTick * 0.07) * 3);
                gUI.drawString("z", WIN_W / 2 + 15, drawY - 2 + zOff);
                gUI.drawString("Z", WIN_W / 2 + 23, drawY - 10 + zOff);
            }

            // Focus bar
            drawFocusBar(gUI);

            // Speech bubble
            if (bubbleTimer > 0 && !bubbleText.isEmpty()) {
                drawBubble(gUI, bubbleText);
            }
        }

        private void drawFocusBar(Graphics2D g2) {
            int barW = WIN_W - 16;
            int barX = 8;
            int barY = WIN_H - 12;

            g2.setColor(new Color(30, 30, 40, 180));
            g2.fillRoundRect(barX, barY, barW, 7, 3, 3);

            Color c = focusScore >= 70 ? new Color(0, 210, 100) :
                      focusScore >= 40 ? new Color(255, 200, 0) : new Color(255, 50, 50);
            int fillW = (int)((focusScore / 100.0) * barW);
            g2.setColor(c);
            g2.fillRoundRect(barX, barY, Math.max(fillW, 4), 7, 3, 3);

            g2.setColor(new Color(255, 255, 255, 200));
            g2.setFont(new Font("Segoe UI", Font.BOLD, 7));
            g2.drawString(String.format("%.0f%%", focusScore), barX + 3, barY + 6);
        }

        private void drawBubble(Graphics2D g2, String text) {
            g2.setFont(new Font("Segoe UI", Font.BOLD, 8));
            FontMetrics fm = g2.getFontMetrics();

            // Word wrap into lines
            String[] words = text.split(" ");
            java.util.List<String> lines = new java.util.ArrayList<>();
            StringBuilder cur = new StringBuilder();
            for (String w : words) {
                if (fm.stringWidth(cur + w + " ") > WIN_W - 24 && cur.length() > 0) {
                    lines.add(cur.toString().trim());
                    cur = new StringBuilder();
                }
                cur.append(w).append(" ");
            }
            if (cur.length() > 0) lines.add(cur.toString().trim());
            if (lines.size() > 3) lines = lines.subList(0, 3); // max 3 lines

            int lineH = fm.getHeight();
            int bh = lines.size() * lineH + 8;
            int bw = WIN_W - 12;
            int bx = 6;
            int by = 2;

            // Fade effect
            float alpha = Math.min(1f, bubbleTimer / 30f);
            g2.setComposite(AlphaComposite.getInstance(AlphaComposite.SRC_OVER, alpha));

            g2.setColor(new Color(18, 18, 25, 220));
            g2.fillRoundRect(bx, by, bw, bh, 6, 6);
            g2.setColor(new Color(255, 255, 255, 210));
            for (int i = 0; i < lines.size(); i++) {
                g2.drawString(lines.get(i), bx + 6, by + 10 + i * lineH);
            }

            g2.setComposite(AlphaComposite.SrcOver);
        }
    }

    // ══════════════════════════════════════════════════
    //  MAIN
    // ══════════════════════════════════════════════════
    public static void main(String[] args) {
        System.setProperty("sun.java2d.opengl", "true");
        SwingUtilities.invokeLater(() -> new MascotOverlay().setVisible(true));
    }
}
