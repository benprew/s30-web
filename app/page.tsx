import { ArrowDownToLine, GitFork, Play, Sparkles } from 'lucide-react';
import Image from 'next/image';
import { Button } from '@/components/ui/button';

const PLAY_URL = 'https://throwingbones.com/ben/s30/';
const RELEASES_URL = 'https://github.com/benprew/s30/releases/latest';

export default function Home() {
  return (
    <main>
      <section className="hero" id="top">
        <nav className="site-nav" aria-label="Main navigation">
          <a className="wordmark" href="#top" aria-label="Shandalar 30 home">
            <span className="wordmark-mark" aria-hidden="true">
              <span>S30</span>
            </span>
            <span>Shandalar</span>
          </a>
          <div className="nav-links">
            <a href="#game">The game</a>
            <a href="#open-source">Open source</a>
            <a
              href="https://github.com/benprew/s30"
              target="_blank"
              rel="noreferrer"
            >
              <GitFork aria-hidden="true" /> GitHub
            </a>
          </div>
        </nav>

        <div className="hero-grid">
          <div className="hero-copy">
            <p className="eyebrow">
              <span /> 30th Anniversary Edition
            </p>
            <h1>
              Shandalar,
              <br />
              <em>reborn.</em>
            </h1>
            <p className="hero-lede">
              Wander a living world. Build your deck. Challenge rival wizards. A
              modern, open-source return to the cult classic adventure.
            </p>
            <div className="hero-actions">
              <Button
                size="lg"
                render={
                  <a
                    href={PLAY_URL}
                    aria-label="Play Shandalar in your browser"
                  />
                }
              >
                <Play fill="currentColor" aria-hidden="true" /> Play in browser
              </Button>
              <Button
                className="outline-button"
                size="lg"
                variant="outline"
                render={
                  <a
                    href={RELEASES_URL}
                    aria-label="Download the latest Shandalar release"
                  />
                }
              >
                <ArrowDownToLine aria-hidden="true" /> Download
              </Button>
            </div>
            <p className="platforms">Windows · macOS · Linux · Web</p>
          </div>

          <div className="map-frame" aria-label="A map of Shandalar">
            <div className="map-crop" />
            <span className="map-caption">THE FIVE DOMINIONS OF SHANDALAR</span>
          </div>
        </div>
        <a className="scroll-cue" href="#game">
          Enter the world <span aria-hidden="true">↓</span>
        </a>
      </section>

      <section className="game-section" id="game">
        <div className="section-intro">
          <p className="section-number">01 / The adventure</p>
          <h2>
            One world.
            <br />
            Endless decks.
          </h2>
          <p>
            Shandalar turns deck building into an adventure. Every road leads to
            a new rival, a better card, or a choice that changes your run.
          </p>
        </div>

        <div className="journey-grid">
          <article>
            <span>EXPLORE</span>
            <h3>Cross a living overworld</h3>
            <p>
              Travel through forests, deserts, mountains, cities, and dungeons
              across a generated isometric realm.
            </p>
          </article>
          <article>
            <span>BUILD</span>
            <h3>Shape your spellbook</h3>
            <p>
              Trade in towns, complete quests, and tune multiple decks as your
              collection grows.
            </p>
          </article>
          <article>
            <span>DUEL</span>
            <h3>Outplay rival wizards</h3>
            <p>
              Fight full rules-engine battles, win cards by ante, and hunt the
              five amulets that guard the plane.
            </p>
          </article>
        </div>
      </section>

      <section className="city-break" aria-label="A city in Shandalar">
        <Image
          src="/images/shandalar-city.png"
          alt="A distant fantasy city beside a lake at sunset"
          fill
          sizes="100vw"
        />
        <div className="city-quote">
          <span aria-hidden="true">“</span>
          <blockquote>Every journey starts with a single card.</blockquote>
        </div>
      </section>

      <section className="source-section" id="open-source">
        <div className="source-copy">
          <p className="section-number">02 / Built in the open</p>
          <h2>
            A classic,
            <br />
            <em>still evolving.</em>
          </h2>
          <p>
            S30 is a free, community-built reimagining written in Go. The world,
            rules engine, AI, and tools are open for anyone to study and
            improve.
          </p>
          <Button
            size="lg"
            variant="outline"
            render={
              <a
                href="https://github.com/benprew/s30"
                target="_blank"
                rel="noreferrer"
                aria-label="Explore the Shandalar source code on GitHub"
              />
            }
          >
            <GitFork aria-hidden="true" /> Explore the source
          </Button>
        </div>
        <div className="source-details">
          <div className="detail-line">
            <span>Engine</span>
            <strong>Go + Ebitengine</strong>
          </div>
          <div className="detail-line">
            <span>Rules</span>
            <strong>mage-go</strong>
          </div>
          <div className="detail-line">
            <span>License</span>
            <strong>GPL v2</strong>
          </div>
          <div className="detail-line">
            <span>Platforms</span>
            <strong>Desktop · Web · Android</strong>
          </div>
        </div>
      </section>

      <section className="final-cta">
        <Sparkles aria-hidden="true" />
        <p>YOUR DECK AWAITS</p>
        <h2>Step into Shandalar.</h2>
        <Button
          size="lg"
          render={<a href={PLAY_URL} aria-label="Play Shandalar now" />}
        >
          <Play fill="currentColor" aria-hidden="true" /> Play now
        </Button>
      </section>

      <footer>
        <a className="wordmark" href="#top" aria-label="Back to the top">
          <span className="wordmark-mark" aria-hidden="true">
            <span>S30</span>
          </span>
          <span>Shandalar</span>
        </a>
        <p>
          An unofficial, non-commercial fan project. Magic: The Gathering is a
          trademark of Wizards of the Coast.
        </p>
        <div>
          <a
            href="https://github.com/benprew/s30/issues"
            target="_blank"
            rel="noreferrer"
          >
            Report an issue
          </a>
          <a
            href="https://github.com/benprew/s30/blob/main/CONTRIBUTING.md"
            target="_blank"
            rel="noreferrer"
          >
            Contribute
          </a>
        </div>
      </footer>
    </main>
  );
}
