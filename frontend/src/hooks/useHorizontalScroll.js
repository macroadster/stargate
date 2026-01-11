import { useRef, useEffect, useState } from 'react';

export const useHorizontalScroll = () => {
  const elRef = useRef();
  const [isDragging, setIsDragging] = useState(false);
  const dragThreshold = 5; // Pixels to move before considering it a drag

  useEffect(() => {
    const el = elRef.current;
    if (el) {
      const onWheel = (e) => {
        if (e.deltaY === 0) return;
        e.preventDefault();
        el.scrollTo({
          left: el.scrollLeft + e.deltaY,
          behavior: 'smooth',
        });
      };

      let isDown = false;
      let startX;
      let scrollLeft;
      let initialPageX;

      const onMouseDown = (e) => {
        isDown = true;
        el.classList.add('active');
        initialPageX = e.pageX;
        startX = e.pageX - el.offsetLeft;
        scrollLeft = el.scrollLeft;
        setIsDragging(false); // Reset dragging state on mouse down
      };

      const onMouseLeave = () => {
        isDown = false;
        el.classList.remove('active');
        el.classList.remove('dragging');
      };

      const onMouseUp = () => {
        isDown = false;
        el.classList.remove('active');
        el.classList.remove('dragging');
      };

      const onMouseMove = (e) => {
        if (!isDown) return;
        e.preventDefault();
        const x = e.pageX - el.offsetLeft;
        const walk = (x - startX); // Raw pixel movement

        // Only set isDragging to true if movement exceeds threshold
        if (Math.abs(e.pageX - initialPageX) > dragThreshold) {
            setIsDragging(true);
        }
        
        el.scrollLeft = scrollLeft - walk;
      };

      el.addEventListener('wheel', onWheel);
      el.addEventListener('mousedown', onMouseDown);
      el.addEventListener('mouseleave', onMouseLeave);
      el.addEventListener('mouseup', onMouseUp);
      el.addEventListener('mousemove', onMouseMove);

      return () => {
        el.removeEventListener('wheel', onWheel);
        el.removeEventListener('mousedown', onMouseDown);
        el.removeEventListener('mouseleave', onMouseLeave);
        el.removeEventListener('mouseup', onMouseUp);
        el.removeEventListener('mousemove', onMouseMove);
      };
    }
  }, []);
  return { elRef, isDragging };
};
