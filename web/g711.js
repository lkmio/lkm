var SIGN_BIT	= 0x80;		/* Sign bit for a A-law byte. */
var QUANT_MASK	= 0xf;		/* Quantization field mask. */
var NSEGS		= 0x8;		/* Number of A-law segments. */
var SEG_SHIFT	= 0x4;		/* Left shift for segment number. */
var SEG_MASK	= 0x70;		/* Segment field mask. */


var seg_aend = new Int16Array([0x1F, 0x3F, 0x7F, 0xFF,0x1FF, 0x3FF, 0x7FF, 0xFFF]);

var seg_uend = new Int16Array([0x3F, 0x7F, 0xFF, 0x1FF,0x3FF, 0x7FF, 0xFFF, 0x1FFF]);


 /* copy from CCITT G.711 specifications */
var _u2a = new Uint8Array([1,	1,	2,	2,	3,	3,	4,	4,
5,	5,	6,	6,	7,	7,	8,	8,
9,	10,	11,	12,	13,	14,	15,	16,
17,	18,	19,	20,	21,	22,	23,	24,
25,	27,	29,	31,	33,	34,	35,	36,
37,	38,	39,	40,	41,	42,	43,	44,
46,	48,	49,	50,	51,	52,	53,	54,
55,	56,	57,	58,	59,	60,	61,	62,
64,	65,	66,	67,	68,	69,	70,	71,
72,	73,	74,	75,	76,	77,	78,	79,
80,	82,	83,	84,	85,	86,	87,	88,
89,	90,	91,	92,	93,	94,	95,	96,
97,	98,	99,	100,	101,	102,	103,	104,
105,	106,	107,	108,	109,	110,	111,	112,
113,	114,	115,	116,	117,	118,	119,	120,
121,	122,	123,	124,	125,	126,	127,	128]);

/* A- to u-law conversions */
var _a2u = new Uint8Array([1,	3,	5,	7,	9,	11,	13,	15,
16,	17,	18,	19,	20,	21,	22,	23,
24,	25,	26,	27,	28,	29,	30,	31,
32,	32,	33,	33,	34,	34,	35,	35,
36,	37,	38,	39,	40,	41,	42,	43,
44,	45,	46,	47,	48,	48,	49,	49,
50,	51,	52,	53,	54,	55,	56,	57,
58,	59,	60,	61,	62,	63,	64,	64,
65,	66,	67,	68,	69,	70,	71,	72,
73,	74,	75,	76,	77,	78,	79,	80,
80,	81,	82,	83,	84,	85,	86,	87,
88,	89,	90,	91,	92,	93,	94,	95,
96,	97,	98,	99,	100,	101,	102,	103,
104,	105,	106,	107,	108,	109,	110,	111,
112,	113,	114,	115,	116,	117,	118,	119,
120,	121,	122,	123,	124,	125,	126,	127]);

function search( val, table, size)
{
    var i;
    
    for (i = 0; i < size; i++) {
        if (val <= table[i])
        return (i);
    }

    return (size);
}

function linear2alaw(pcm_val)	/* 2's complement (16-bit range) */
{
var	 mask;
var	 seg;
var aval;

pcm_val = pcm_val >> 3;

if (pcm_val >= 0) {
  mask = 0xD5;		/* sign (7th) bit = 1 */
} else {
  mask = 0x55;		/* sign bit = 0 */
  pcm_val = -pcm_val - 1;
}

/* Convert the scaled magnitude to segment number. */
seg = search(pcm_val, seg_aend, 8);

/* Combine the sign, segment, and quantization bits. */

if (seg >= 8)		/* out of range, return maximum value. */
  return (0x7F ^ mask);
else {
  aval = seg << SEG_SHIFT;
  if (seg < 2)
 aval |= (pcm_val >> 1) & QUANT_MASK;
  else
 aval |= (pcm_val >> seg) & QUANT_MASK;
  return (aval ^ mask);
}
}

/*
* alaw2linear() - Convert an A-law value to 16-bit linear PCM
*
*/

function alaw2linear(a_val){
var t;
var seg;

a_val ^= 0x55;

t = (a_val & QUANT_MASK) << 4;
seg = (a_val & SEG_MASK) >> SEG_SHIFT;
switch (seg) {
case 0:
  t += 8;
  break;
case 1:
  t += 0x108;
  break;
default:
  t += 0x108;
  t <<= seg - 1;
}
return ((a_val & SIGN_BIT) ? t : -t);
}


// #define	BIAS		(0x84)		/* Bias for linear code. */
// #define CLIP            8159
var BIAS = 0x84;

var CLIP = 8159;

// /*
// * linear2ulaw() - Convert a linear PCM value to u-law
// *
// * In order to simplify the encoding process, the original linear magnitude
// * is biased by adding 33 which shifts the encoding range from (0 - 8158) to
// * (33 - 8191). The result can be seen in the following encoding table:
// *
// *	Biased Linear Input Code	Compressed Code
// *	------------------------	---------------
// *	00000001wxyza			000wxyz
// *	0000001wxyzab			001wxyz
// *	000001wxyzabc			010wxyz
// *	00001wxyzabcd			011wxyz
// *	0001wxyzabcde			100wxyz
// *	001wxyzabcdef			101wxyz
// *	01wxyzabcdefg			110wxyz
// *	1wxyzabcdefgh			111wxyz
// *
// * Each biased linear code has a leading 1 which identifies the segment
// * number. The value of the segment number is equal to 7 minus the number
// * of leading 0's. The quantization interval is directly available as the
// * four bits wxyz.  * The trailing bits (a - h) are ignored.
// *
// * Ordinarily the complement of the resulting code word is used for
// * transmission, and so the code word is complemented before it is returned.
// *
// * For further information see John C. Bellamy's Digital Telephony, 1982,
// * John Wiley & Sons, pps 98-111 and 472-476.
function linear2ulaw(pcm_val)	/* 2's complement (16-bit range) */
{
var         mask;
var	 seg;
var uval;

/* Get the sign and the magnitude of the value. */
pcm_val = pcm_val >> 2;
if (pcm_val < 0) {
  pcm_val = -pcm_val;
  mask = 0x7F;
} else {
  mask = 0xFF;
}
if ( pcm_val > CLIP ) pcm_val = CLIP;		/* clip the magnitude */
pcm_val += (BIAS >> 2);

/* Convert the scaled magnitude to segment number. */
seg = search(pcm_val, seg_uend, 8);

/*
* Combine the sign, segment, quantization bits;
* and complement the code word.
*/
if (seg >= 8)		/* out of range, return maximum value. */
  return (0x7F ^ mask);
else {
  uval = (seg << 4) | ((pcm_val >> (seg + 1)) & 0xF);
  return (uval ^ mask);
}

}



// /*
//  * ulaw2linear() - Convert a u-law value to 16-bit linear PCM
//  *
//  * First, a biased linear code is derived from the code word. An unbiased
//  * output can then be obtained by subtracting 33 from the biased code.
//  *
//  * Note that this function expects to be passed the complement of the
//  * original code word. This is in keeping with ISDN conventions.
//  */
function ulaw2linear(u_val)
{
var t;

/* Complement to obtain normal u-law value. */
u_val = ~u_val;

/*
* Extract and bias the quantization bits. Then
* shift up by the segment number and subtract out the bias.
*/
t = ((u_val & QUANT_MASK) << 3) + BIAS;
t <<= (u_val & SEG_MASK) >> SEG_SHIFT;

return ((u_val & SIGN_BIT) ? (BIAS - t) : (t - BIAS));
}

// /* A-law to u-law conversion */
function alaw2ulaw(aval)
{
aval &= 0xff;
return  ((aval & 0x80) ? (0xFF ^ _a2u[aval ^ 0xD5]) :
   (0x7F ^ _a2u[aval ^ 0x55]));
}

/* u-law to A-law conversion */
function ulaw2alaw(uval)
{
uval &= 0xff;
return  ((uval & 0x80) ? (0xD5 ^ (_u2a[0xFF ^ uval] - 1)) :
           (0x55 ^ (_u2a[0x7F ^ uval] - 1)));
}

// unsigned char linear_to_alaw[65536];
// unsigned char linear_to_ulaw[65536];

var short_index = new Int16Array(65536);

var linear_to_alaw = new Uint8Array(65536);
var linear_to_ulaw = new Uint8Array(65536);

// /* 16384 entries per table (8 bit) */
// unsigned short alaw_to_linear[256];
// unsigned short ulaw_to_linear[256];

var alaw_to_linear =  new Uint8Array(256);
var ulaw_to_linear =  new Uint8Array(256);

function  build_linear_to_xlaw_table(linear_to_xlaw,linear2xlaw)
{
    var i;
    for (i=0; i<65536;i++){
        var v = linear2xlaw(short_index[i]);
        linear_to_xlaw[i] = v;
    }
}


function build_xlaw_to_linear_table(xlaw_to_linear,xlaw2linear)
{
    var i;

    for (i=0; i<256;i++){
        xlaw_to_linear[i] =  xlaw2linear(i);
    }
}

function pcm16_to_xlaw(linear_to_xlaw,  src_length,src_samples,dst_samples)
{
    var i;
    var s_samples;

    s_samples = src_samples;

    for (i=0; i < src_length / 2; i++)
    {
        dst_samples[i] = linear_to_xlaw[s_samples[i]];
    }
}

function xlaw_to_pcm16(xlaw_to_linear, src_length,src_samples, dst_samples)
{
    var i;
    var  s_samples;
    var d_samples;

    s_samples =  src_samples;
    d_samples = dst_samples;

    for (i=0; i < src_length; i++)
    {
        d_samples[i] = xlaw_to_linear[s_samples[i]];
    }
}

function pcm16_to_alaw(src_length, src_samples, dst_samples)
{
    pcm16_to_xlaw(linear_to_alaw, src_length, src_samples, dst_samples);
}

function pcm16_to_ulaw(src_length, src_samples, dst_samples)
{
    pcm16_to_xlaw(linear_to_ulaw, src_length, src_samples, dst_samples);
}

function alaw_to_pcm16(src_length, src_samples, dst_samples)
{
    xlaw_to_pcm16(alaw_to_linear, src_length, src_samples, dst_samples);
}

function ulaw_to_pcm16(src_length, src_samples, dst_samples)
{
    xlaw_to_pcm16(ulaw_to_linear, src_length, src_samples, dst_samples);
}

function pcm16_alaw_tableinit()
{
    build_linear_to_xlaw_table(linear_to_alaw, linear2alaw);
}

function pcm16_ulaw_tableinit()
{
    build_linear_to_xlaw_table(linear_to_ulaw, linear2ulaw);
}

function alaw_pcm16_tableinit()
{
    build_xlaw_to_linear_table(alaw_to_linear, alaw2linear);
}

function ulaw_pcm16_tableinit()
{
    build_xlaw_to_linear_table(ulaw_to_linear, ulaw2linear);
}

for(var i =0; i < 65536;i++){
    short_index[i] = i;
}

pcm16_alaw_tableinit();
pcm16_ulaw_tableinit();
alaw_pcm16_tableinit();
ulaw_pcm16_tableinit();