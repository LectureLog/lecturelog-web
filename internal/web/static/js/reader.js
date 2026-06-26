(() => {
  const root = document.getElementById('reader');
  if (!root) return;

  const selectAll = (selector, parent = root) => Array.from(parent.querySelectorAll(selector));
  const content = document.getElementById('readerContent');
  const links = selectAll('.ll-reader-toc-link');
  const subtopics = selectAll('.ll-reader-sub');
  const progress = document.getElementById('readerProgress');
  const readBar = document.getElementById('readerReadBar');
  const readPercent = document.getElementById('readerReadPercent');

  const updateReading = () => {
    const maximum = document.documentElement.scrollHeight - window.innerHeight;
    const percent = maximum > 0 ? Math.min(100, Math.max(0, window.scrollY / maximum * 100)) : 0;
    progress.style.width = `${percent}%`;
    readBar.style.width = `${percent}%`;
    readPercent.textContent = `${Math.round(percent)}%`;

    let active = subtopics[0];
    subtopics.forEach((subtopic) => {
      if (subtopic.getBoundingClientRect().top <= 132) active = subtopic;
    });
    links.forEach((link) => link.classList.toggle('active', active && link.dataset.target === active.id));
  };

  window.addEventListener('scroll', updateReading, { passive: true });
  window.addEventListener('resize', updateReading);
  updateReading();

  links.forEach((link) => link.addEventListener('click', (event) => {
    event.preventDefault();
    const target = document.getElementById(link.dataset.target);
    if (!target) return;
    window.scrollTo({ top: target.getBoundingClientRect().top + window.scrollY - 72, behavior: 'smooth' });
  }));

  selectAll('.ll-reader-player').forEach((player) => {
    const media = player.querySelector('audio, video');
    if (!media) return;
    const start = Number(player.dataset.start || 0);
    const end = Number(player.dataset.end || 0);
    media.addEventListener('play', () => {
      if (media.currentTime < start || (end && media.currentTime >= end)) media.currentTime = start;
    });
    media.addEventListener('timeupdate', () => {
      if (end && media.currentTime >= end) {
        media.pause();
        media.currentTime = start;
      }
    });
  });

  const lightbox = document.getElementById('readerLightbox');
  const lightboxClose = lightbox.querySelector('.ll-reader-lightbox-close');
  let lightboxTrigger = null;
  const closeLightbox = () => {
    if (lightbox.hidden) return;
    lightbox.hidden = true;
    lightboxTrigger?.focus();
    lightboxTrigger = null;
  };
  const openLightbox = (image) => {
    lightboxTrigger = image;
    lightbox.replaceChildren(lightboxClose, image.cloneNode());
    lightbox.hidden = false;
    lightboxClose.focus();
  };
  selectAll('.ll-reader-slide img').forEach((image) => {
    image.tabIndex = 0;
    image.setAttribute('role', 'button');
    image.setAttribute('aria-label', `Открыть слайд: ${image.alt}`);
    image.addEventListener('click', () => openLightbox(image));
    image.addEventListener('keydown', (event) => {
      if (event.key !== 'Enter' && event.key !== ' ') return;
      event.preventDefault();
      openLightbox(image);
    });
  });
  lightboxClose.addEventListener('click', closeLightbox);
  lightbox.addEventListener('click', (event) => {
    if (event.target === lightbox) closeLightbox();
  });
  document.addEventListener('keydown', (event) => {
    if (lightbox.hidden) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      closeLightbox();
    }
    if (event.key === 'Tab') {
      event.preventDefault();
      lightboxClose.focus();
    }
  });

  const searchButton = document.getElementById('readerSearchButton');
  const search = document.getElementById('readerSearch');
  const searchInput = document.getElementById('readerSearchInput');
  const searchCount = document.getElementById('readerSearchCount');
  const previous = document.getElementById('readerSearchPrev');
  const next = document.getElementById('readerSearchNext');
  const close = document.getElementById('readerSearchClose');
  let matches = [];
  let matchIndex = -1;

  const clearSearch = () => {
    selectAll('mark', content).forEach((mark) => mark.replaceWith(document.createTextNode(mark.textContent)));
    content.normalize();
    matches = [];
    matchIndex = -1;
    searchCount.textContent = '';
  };
  const goToMatch = (index) => {
    if (!matches.length) return;
    matchIndex = (index + matches.length) % matches.length;
    matches.forEach((mark) => mark.classList.remove('ll-reader-mark-current'));
    const match = matches[matchIndex];
    match.classList.add('ll-reader-mark-current');
    searchCount.textContent = `${matchIndex + 1} / ${matches.length}`;
    window.scrollTo({ top: match.getBoundingClientRect().top + window.scrollY - 120, behavior: 'smooth' });
  };
  const runSearch = (term) => {
    clearSearch();
    const normalized = term.trim().toLocaleLowerCase();
    if (normalized.length < 2) return;
    const walker = document.createTreeWalker(content, NodeFilter.SHOW_TEXT, {
      acceptNode: (node) => node.parentElement.closest('script, style, mark') || !node.nodeValue.trim() || !node.nodeValue.toLocaleLowerCase().includes(normalized)
        ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT,
    });
    const nodes = [];
    for (let node = walker.nextNode(); node; node = walker.nextNode()) nodes.push(node);
    nodes.forEach((node) => {
      const text = node.nodeValue;
      const lower = text.toLocaleLowerCase();
      const fragment = document.createDocumentFragment();
      let offset = 0;
      for (let position = lower.indexOf(normalized, offset); position !== -1; position = lower.indexOf(normalized, offset)) {
        fragment.append(text.slice(offset, position));
        const mark = document.createElement('mark');
        mark.textContent = text.slice(position, position + normalized.length);
        fragment.append(mark);
        matches.push(mark);
        offset = position + normalized.length;
      }
      fragment.append(text.slice(offset));
      node.replaceWith(fragment);
    });
    if (matches.length) goToMatch(0); else searchCount.textContent = 'нет совпадений';
  };

  searchButton.addEventListener('click', () => {
    search.hidden = !search.hidden;
    if (!search.hidden) searchInput.focus(); else clearSearch();
  });
  close.addEventListener('click', () => { search.hidden = true; searchInput.value = ''; clearSearch(); });
  next.addEventListener('click', () => goToMatch(matchIndex + 1));
  previous.addEventListener('click', () => goToMatch(matchIndex - 1));
  searchInput.addEventListener('input', () => runSearch(searchInput.value));
  searchInput.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') { event.preventDefault(); goToMatch(matchIndex + (event.shiftKey ? -1 : 1)); }
    if (event.key === 'Escape') close.click();
  });
})();
