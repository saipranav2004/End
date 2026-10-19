import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';

/**
 * Popover behaviour: dismissal, and placement against the space available.
 *
 * Every dropdown in the console used to open downward at a fixed height, so a
 * tall one on a short window ran off the bottom of the screen and the last
 * options were unreachable without scrolling the page behind it. This measures
 * the room below the trigger and above it, then either caps the panel's height
 * to what is there or flips it above the trigger when that is roomier.
 *
 * It re-measures on scroll and resize, because either can change the answer
 * while the panel is open.
 *
 * Returns:
 *  - `wrapperRef`  on the positioned wrapper (the element the panel is
 *                  absolutely positioned inside)
 *  - `triggerRef`  on the button that opens it
 *  - `placement`   'bottom' | 'top'
 *  - `maxHeight`   px budget for the panel, or undefined before first measure
 *  - `panelProps`  spread onto the panel: the placement classes and max-height
 */
const MARGIN = 12; /* breathing room against the viewport edge */
const MIN_USABLE = 168; /* below this a flip is always better than a squeeze */

export function usePopover(open, onDismiss) {
  const wrapperRef = useRef(null);
  const triggerRef = useRef(null);
  const [placement, setPlacement] = useState('bottom');
  const [maxHeight, setMaxHeight] = useState(undefined);

  const measure = useCallback(() => {
    const trigger = triggerRef.current ?? wrapperRef.current;
    if (!trigger) return;
    const rect = trigger.getBoundingClientRect();
    /* `visualViewport` is the honest number on a phone with the keyboard up. */
    const viewportHeight = window.visualViewport?.height ?? window.innerHeight;
    const below = viewportHeight - rect.bottom - MARGIN;
    const above = rect.top - MARGIN;

    if (below >= MIN_USABLE || below >= above) {
      setPlacement('bottom');
      setMaxHeight(Math.max(MIN_USABLE, Math.floor(below)));
    } else {
      setPlacement('top');
      setMaxHeight(Math.max(MIN_USABLE, Math.floor(above)));
    }
  }, []);

  /* Measured before paint, so the panel never appears in the wrong place. */
  useLayoutEffect(() => {
    if (open) measure();
  }, [open, measure]);

  useEffect(() => {
    if (!open) return undefined;

    const onPointerDown = (event) => {
      if (!wrapperRef.current?.contains(event.target)) onDismiss?.();
    };
    const onKeyDown = (event) => {
      if (event.key === 'Escape') onDismiss?.();
    };

    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    /* Capture phase: a scroll inside the page, not just on the window. */
    window.addEventListener('scroll', measure, true);
    window.addEventListener('resize', measure);
    window.visualViewport?.addEventListener('resize', measure);

    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('scroll', measure, true);
      window.removeEventListener('resize', measure);
      window.visualViewport?.removeEventListener('resize', measure);
    };
  }, [open, onDismiss, measure]);

  const panelProps = {
    'data-placement': placement,
    style: maxHeight ? { maxHeight: `${maxHeight}px` } : undefined,
  };

  return { wrapperRef, triggerRef, placement, maxHeight, panelProps, remeasure: measure };
}
