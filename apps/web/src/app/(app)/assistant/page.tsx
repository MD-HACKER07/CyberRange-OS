"use client";

import { useEffect, useRef, useState } from "react";
import { Mic, MicOff, Send, Volume2, VolumeX, BookOpen, Bot } from "lucide-react";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth-context";
import { PageHeader } from "@/components/page-header";
import { Card, SectionTitle, Button, Input, Textarea, Spinner, Badge } from "@/components/ui";

interface Turn {
  role: "user" | "assistant";
  content: string;
}

// Minimal typing for the browser Web Speech API (not in lib.dom by default).
type SpeechRecognitionLike = {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  onresult: ((e: any) => void) | null;
  onend: (() => void) | null;
  onerror: ((e: any) => void) | null;
  start: () => void;
  stop: () => void;
};

export default function AssistantPage() {
  const { user } = useAuth();
  const isStaff = user?.role === "faculty" || user?.role === "admin";

  const [turns, setTurns] = useState<Turn[]>([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [listening, setListening] = useState(false);
  const [speak, setSpeak] = useState(true);
  const [supported, setSupported] = useState(true);

  const recRef = useRef<SpeechRecognitionLike | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const w = window as any;
    const SR = w.SpeechRecognition || w.webkitSpeechRecognition;
    if (!SR) {
      setSupported(false);
      return;
    }
    const rec: SpeechRecognitionLike = new SR();
    rec.lang = "en-US";
    rec.continuous = false;
    rec.interimResults = false;
    rec.onresult = (e: any) => {
      const transcript = Array.from(e.results)
        .map((r: any) => r[0].transcript)
        .join(" ")
        .trim();
      if (transcript) {
        setInput(transcript);
        void send(transcript);
      }
    };
    rec.onend = () => setListening(false);
    rec.onerror = () => setListening(false);
    recRef.current = rec;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    scrollRef.current?.scrollTo(0, scrollRef.current.scrollHeight);
  }, [turns, busy]);

  const speakText = (text: string) => {
    if (!speak || typeof window === "undefined" || !window.speechSynthesis) return;
    window.speechSynthesis.cancel();
    const u = new SpeechSynthesisUtterance(text);
    u.lang = "en-US";
    u.rate = 1.02;
    window.speechSynthesis.speak(u);
  };

  const toggleMic = () => {
    const rec = recRef.current;
    if (!rec) return;
    if (listening) {
      rec.stop();
      setListening(false);
    } else {
      try {
        window.speechSynthesis?.cancel();
        rec.start();
        setListening(true);
      } catch {
        setListening(false);
      }
    }
  };

  const send = async (q?: string) => {
    const question = (q ?? input).trim();
    if (!question || busy) return;
    setInput("");
    const history = turns.slice(-6);
    setTurns((prev) => [...prev, { role: "user", content: question }]);
    setBusy(true);
    try {
      const res = await api<{ answer: string }>("/assistant/ask", {
        body: { question, history },
      });
      setTurns((prev) => [...prev, { role: "assistant", content: res.answer }]);
      speakText(res.answer);
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Assistant unavailable";
      setTurns((prev) => [...prev, { role: "assistant", content: "⚠️ " + msg }]);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="p-8">
      <PageHeader
        title="Department Assistant"
        subtitle="Ask about routines, schedules, or the platform — by voice or text. Answered by the local model."
        actions={
          <Button variant="ghost" onClick={() => setSpeak((s) => !s)} title="Toggle spoken replies">
            {speak ? <Volume2 size={16} /> : <VolumeX size={16} />}
            {speak ? "Voice on" : "Voice off"}
          </Button>
        }
      />

      <div className="grid lg:grid-cols-3 gap-4">
        <Card className="lg:col-span-2 flex flex-col" accent="gold">
          <SectionTitle>
            <span className="flex items-center gap-2">
              <Bot size={14} /> Assistant
            </span>
          </SectionTitle>

          <div ref={scrollRef} className="flex-1 overflow-auto space-y-3 min-h-[24rem] max-h-[55vh] pr-1">
            {turns.length === 0 && (
              <div className="text-sm text-vault-white/40 space-y-2">
                <p>Try asking:</p>
                <ul className="list-disc pl-5 space-y-1">
                  <li>&ldquo;What are the lab timings?&rdquo;</li>
                  <li>&ldquo;How do I start a Red Team session?&rdquo;</li>
                  <li>&ldquo;Who do I contact for enrollment?&rdquo;</li>
                  <li>&ldquo;When is the next exam?&rdquo;</li>
                </ul>
              </div>
            )}
            {turns.map((t, i) => (
              <div key={i} className={t.role === "user" ? "text-right" : "text-left"}>
                <div
                  className={
                    "inline-block rounded-lg px-3 py-2 text-sm max-w-[85%] " +
                    (t.role === "user"
                      ? "bg-vault-gold/15 text-vault-white"
                      : "bg-black border border-vault-slate text-vault-white/90")
                  }
                >
                  {t.content}
                </div>
              </div>
            ))}
            {busy && (
              <div className="text-left">
                <div className="inline-block rounded-lg px-3 py-2 bg-black border border-vault-slate">
                  <Spinner label="Thinking…" />
                </div>
              </div>
            )}
          </div>

          <form
            onSubmit={(e) => {
              e.preventDefault();
              void send();
            }}
            className="flex items-center gap-2 mt-3"
          >
            <button
              type="button"
              onClick={toggleMic}
              disabled={!supported}
              title={supported ? "Hold a conversation by voice" : "Voice input not supported in this browser"}
              className={
                "rounded-md p-2 border transition-colors " +
                (listening
                  ? "border-vault-red text-vault-red animate-pulse-glow"
                  : "border-vault-gold/40 text-vault-gold hover:bg-vault-gold/10")
              }
            >
              {listening ? <MicOff size={18} /> : <Mic size={18} />}
            </button>
            <Input
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder={listening ? "Listening…" : "Ask anything about the department…"}
            />
            <Button type="submit" disabled={busy}>
              <Send size={16} /> Ask
            </Button>
          </form>
          {!supported && (
            <p className="text-xs text-vault-white/40 mt-2">
              Voice input needs Chrome or Edge. You can still type your questions.
            </p>
          )}
        </Card>

        <div className="space-y-4">
          <Card>
            <SectionTitle>How it works</SectionTitle>
            <ul className="text-sm text-vault-white/60 space-y-2 list-disc pl-5">
              <li>Click the mic and speak, or type your question.</li>
              <li>Answers are generated by the department&apos;s local model.</li>
              <li>Replies are read aloud; toggle voice with the speaker button.</li>
            </ul>
          </Card>

          {isStaff && <KnowledgeEditor />}
        </div>
      </div>
    </div>
  );
}

function KnowledgeEditor() {
  const [content, setContent] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api<{ content: string }>("/assistant/knowledge")
      .then((d) => {
        setContent(d.content);
        setLoaded(true);
      })
      .catch(() => setLoaded(true));
  }, []);

  const save = async () => {
    setSaving(true);
    setSaved(false);
    try {
      await api("/assistant/knowledge", { method: "PUT", body: { content } });
      setSaved(true);
      setTimeout(() => setSaved(false), 2500);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card>
      <SectionTitle
        action={
          <Button variant="outline" onClick={save} disabled={saving || !loaded}>
            <BookOpen size={14} /> {saving ? "Saving…" : "Save"}
          </Button>
        }
      >
        Knowledge Base
      </SectionTitle>
      <p className="text-xs text-vault-white/50 mb-2">
        Faculty/Admin only. Add your department&apos;s real routine, timings, contacts, and rules here — the assistant
        answers from it. {saved && <Badge accent="gold">saved</Badge>}
      </p>
      {loaded ? (
        <Textarea rows={16} value={content} onChange={(e) => setContent(e.target.value)} className="text-xs" />
      ) : (
        <Spinner label="Loading…" />
      )}
    </Card>
  );
}
